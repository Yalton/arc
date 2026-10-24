package imports

import (
	"path"
	"strings"
)

// pythonImports parses `import a.b`, `from a.b import c` and relative
// `from . import x` statements, including parenthesised and
// backslash-continued ones.
func pythonImports(root, file string, data []byte) []Import {
	out := []Import{}
	for _, st := range pythonStatements(string(data)) {
		for _, s := range strings.Split(st.text, ";") {
			out = append(out, pythonStatement(root, file, strings.TrimSpace(s), st.line)...)
		}
	}
	return out
}

type pyStmt struct {
	text string
	line int
}

// pythonStatements joins source into logical lines with comments and string
// literals removed, keeping only those that start with import or from.
func pythonStatements(src string) []pyStmt {
	var (
		out    []pyStmt
		cur    strings.Builder
		start  int
		depth  int
		cont   bool
		triple string // closing delimiter of an open triple-quoted string
	)
	for i, line := range strings.Split(src, "\n") {
		if triple != "" {
			j := strings.Index(line, triple)
			if j < 0 {
				continue
			}
			line, triple = line[j+3:], ""
		}
		code, open, delta := stripPython(line)
		triple = open
		if depth == 0 && !cont {
			cur.Reset()
			start = i + 1
		}
		code = strings.TrimRight(code, " \t\r")
		cont = strings.HasSuffix(code, `\`)
		cur.WriteString(" ")
		cur.WriteString(strings.TrimSuffix(code, `\`))
		if depth += delta; depth < 0 {
			depth = 0
		}
		if depth == 0 && !cont {
			s := strings.TrimSpace(cur.String())
			if strings.HasPrefix(s, "import ") || strings.HasPrefix(s, "from ") {
				out = append(out, pyStmt{s, start})
			}
		}
	}
	return out
}

// stripPython drops a line's comment and string literal contents, returning
// the remaining code, the closing delimiter of a triple-quoted string left
// open at the end of the line, and the net bracket depth change.
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
			end := strings.Index(line[i+len(q):], q)
			if end < 0 {
				if len(q) == 3 {
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

// pythonStatement resolves one import or from statement.
func pythonStatement(root, file, s string, line int) []Import {
	var out []Import
	seen := map[string]bool{}
	add := func(raw, p string) {
		if seen[raw+"\x00"+p] {
			return
		}
		seen[raw+"\x00"+p] = true
		out = append(out, Import{Path: p, Raw: raw, Line: line})
	}
	switch {
	case strings.HasPrefix(s, "import "):
		for _, part := range strings.Split(s[len("import "):], ",") {
			if f := strings.Fields(part); len(f) > 0 {
				add(f[0], resolvePython(root, file, f[0]))
			}
		}
	case strings.HasPrefix(s, "from "):
		mod, names, ok := strings.Cut(s[len("from "):], " import ")
		mod = strings.TrimSpace(mod)
		if !ok || mod == "" {
			return nil
		}
		names = strings.Trim(strings.TrimSpace(names), "()")
		for _, part := range strings.Split(names, ",") {
			f := strings.Fields(part)
			if len(f) == 0 || f[0] == "*" {
				continue
			}
			// "from pkg import name" imports the submodule pkg.name when
			// one exists, otherwise a name from pkg.
			sub := mod + "." + f[0]
			if strings.HasSuffix(mod, ".") {
				sub = mod + f[0]
			}
			if p := resolvePython(root, file, sub); p != "" && pythonExists(root, p) {
				add(sub, p)
			} else {
				add(mod, resolvePython(root, file, mod))
			}
		}
		if len(out) == 0 {
			add(mod, resolvePython(root, file, mod))
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
