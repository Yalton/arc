package imports

import (
	"path"
	"strings"
)

// pythonImports parses `import a.b`, `from a.b import c` and relative
// `from . import x` statements, including parenthesised and
// backslash-continued ones. Each import's Line is the line its module or
// name token is on, not the statement's first line.
func pythonImports(root, file string, data []byte) []Import {
	out := []Import{}
	for _, st := range pythonStatements(string(data)) {
		var seg []pyTok
		for _, t := range append(st, pyTok{";", 0}) {
			if t.text != ";" {
				seg = append(seg, t)
				continue
			}
			out = append(out, pythonStatement(root, file, seg)...)
			seg = nil
		}
	}
	return out
}

// pyTok is a token of a logical line with the source line it is on.
// Names include dots ("pkg.mod", ".."); other tokens are single characters.
type pyTok struct {
	text string
	line int
}

// pythonStatements joins source into logical lines of tokens with comments
// and string literals removed.
func pythonStatements(src string) [][]pyTok {
	var (
		out   [][]pyTok
		cur   []pyTok
		depth int
		cont  bool
		open  string // closing delimiter of a string left open at line end
	)
	for i, line := range strings.Split(src, "\n") {
		if open != "" {
			j := pyStringEnd(line, open)
			if j < 0 {
				if len(open) == 1 && !strings.HasSuffix(line, `\`) {
					open, cont = "", false // unterminated single-quoted string
				}
				continue
			}
			line, open = line[j+len(open):], ""
		}
		code, o, delta := stripPython(line)
		open = o
		if depth == 0 && !cont {
			cur = nil
		}
		code = strings.TrimRight(code, " \t\r")
		cont = strings.HasSuffix(code, `\`)
		cur = append(cur, pyTokens(strings.TrimSuffix(code, `\`), i+1)...)
		if depth += delta; depth < 0 {
			depth = 0
		}
		if open != "" && len(open) == 1 {
			cont = true // string continued with a trailing backslash
		}
		if depth == 0 && !cont && len(cur) > 0 {
			out = append(out, cur)
		}
	}
	return out
}

// pyTokens splits stripped code into tokens on source line n.
func pyTokens(code string, n int) []pyTok {
	var out []pyTok
	isName := func(c byte) bool {
		return c == '_' || c == '.' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' ||
			c >= 'A' && c <= 'Z' || c >= 0x80
	}
	for i := 0; i < len(code); {
		c := code[i]
		switch {
		case c == ' ' || c == '\t' || c == '\r' || c == '\\':
			i++
		case isName(c):
			j := i
			for j < len(code) && isName(code[j]) {
				j++
			}
			out = append(out, pyTok{code[i:j], n})
			i = j
		default:
			out = append(out, pyTok{code[i : i+1], n})
			i++
		}
	}
	return out
}

// stripPython drops a line's comment and string literal contents, returning
// the remaining code, the closing delimiter of a string left open at the
// end of the line (a triple-quoted one, or a single-quoted one continued
// with a backslash), and the net bracket depth change.
func stripPython(line string) (code, open string, delta int) {
	var b strings.Builder
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch c {
		case '#':
			return b.String(), "", delta
		case '(', '[', '{':
			delta++
		case ')', ']', '}':
			delta--
		case '\'', '"':
			q := string(c)
			if strings.HasPrefix(line[i:], q+q+q) {
				q = q + q + q
			}
			end := pyStringEnd(line[i+len(q):], q)
			if end < 0 {
				if len(q) == 3 || strings.HasSuffix(line, `\`) {
					return b.String(), q, delta
				}
				return b.String(), "", delta
			}
			b.WriteString(`""`)
			i += len(q) + end + len(q) - 1
			continue
		}
		b.WriteByte(c)
	}
	return b.String(), "", delta
}

// pyStringEnd returns the index in s of closing delimiter q, skipping
// backslash escapes, or -1.
func pyStringEnd(s, q string) int {
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '\\':
			i++
		case strings.HasPrefix(s[i:], q):
			return i
		}
	}
	return -1
}

// pythonStatement resolves one import or from statement.
func pythonStatement(root, file string, toks []pyTok) []Import {
	if len(toks) == 0 {
		return nil
	}
	var out []Import
	seen := map[string]bool{}
	add := func(raw, p string, line int) {
		if seen[raw+"\x00"+p] {
			return
		}
		seen[raw+"\x00"+p] = true
		out = append(out, Import{Path: p, Raw: raw, Line: line})
	}
	switch toks[0].text {
	case "import":
		for _, part := range pySplit(toks[1:]) {
			add(part[0].text, resolvePython(root, file, part[0].text), part[0].line)
		}
	case "from":
		var mod string
		modLine, i := 0, 1
		for ; i < len(toks) && toks[i].text != "import"; i++ {
			if modLine == 0 {
				modLine = toks[i].line
			}
			mod += toks[i].text
		}
		if i == len(toks) || mod == "" {
			return nil
		}
		for _, part := range pySplit(toks[i+1:]) {
			name := part[0]
			if name.text == "*" {
				continue
			}
			// "from pkg import name" imports the submodule pkg.name when
			// one exists, otherwise a name from pkg.
			sub := mod + "." + name.text
			if strings.HasSuffix(mod, ".") {
				sub = mod + name.text
			}
			if p := resolvePython(root, file, sub); p != "" && pythonExists(root, p) {
				add(sub, p, name.line)
			} else {
				add(mod, resolvePython(root, file, mod), modLine)
			}
		}
		if len(out) == 0 {
			add(mod, resolvePython(root, file, mod), modLine)
		}
	}
	return out
}

// pySplit splits tokens at commas, dropping brackets and empty parts.
func pySplit(toks []pyTok) [][]pyTok {
	var out [][]pyTok
	var cur []pyTok
	for _, t := range append(toks, pyTok{",", 0}) {
		switch t.text {
		case "(", ")":
		case ",":
			if len(cur) > 0 {
				out = append(out, cur)
			}
			cur = nil
		default:
			cur = append(cur, t)
		}
	}
	return out
}

// resolvePython resolves a dotted module name. Absolute names are tried
// against the repository root and a src/ layout and are external when not
// found. Relative names resolve against the importing file's package; when
// the target does not exist the computed path is still returned.
func resolvePython(root, file, mod string) string {
	dots := len(mod) - len(strings.TrimLeft(mod, "."))
	rest := strings.ReplaceAll(mod[dots:], ".", "/")
	if dots == 0 {
		for _, base := range []string{"", "src/"} {
			if p := pythonModule(root, base+rest); p != "" {
				return p
			}
		}
		return ""
	}
	base := path.Dir(file)
	for i := 1; i < dots; i++ {
		base = path.Dir(base)
	}
	p, ok := inRepo(path.Join(base, rest))
	if !ok {
		return ""
	}
	if found := pythonModule(root, p); found != "" {
		return found
	}
	return p
}

// pythonModule returns the module file (a/b.py) or package dir (a/b, with
// or without __init__.py) for slash path p, or "" if neither exists.
func pythonModule(root, p string) string {
	switch {
	case p == "" || p == ".":
		return ""
	case isFile(root, p+".py"):
		return p + ".py"
	case isDir(root, p):
		return p
	}
	return ""
}

func pythonExists(root, p string) bool { return isFile(root, p) || isDir(root, p) }
