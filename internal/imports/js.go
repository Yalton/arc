package imports

import (
	"path"
	"regexp"
	"sort"
	"strings"
)

// jsImportRe matches the module specifier of `import ... from "x"`,
// `export ... from "x"`, `import "x"`, dynamic `import("x")` and
// `require("x")`. Statements may span lines.
var jsImportRe = regexp.MustCompile(`(?:\b(?:import|export)\s[^'"` + "`" + `;]*?\bfrom\s*|\bimport\s*\(?\s*|\brequire\s*\(\s*)(?:"([^"\n]+)"|'([^'\n]+)')`)

var jsExts = []string{".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs", ".mts", ".cts"}

// jsImports finds the imports in TS/JS source, ignoring comments and
// matches that start inside a string literal.
// Relative specifiers resolve to repo files; bare ones are external (tsconfig
// paths are not read).
func jsImports(root, file string, data []byte) []Import {
	src, strs := blankJSComments(string(data))
	out := []Import{}
	for _, m := range jsImportRe.FindAllStringSubmatchIndex(src, -1) {
		i := sort.Search(len(strs), func(i int) bool { return strs[i][1] > m[0] })
		if i < len(strs) && strs[i][0] < m[0] {
			continue
		}
		s, e := m[2], m[3]
		if s < 0 {
			s, e = m[4], m[5]
		}
		raw := src[s:e]
		out = append(out, Import{Path: resolveJS(root, file, raw), Raw: raw, Line: lineAt(src, s)})
	}
	return out
}

// resolveJS resolves a relative specifier by trying it as written, with each
// extension, a TS source behind a .js specifier, and as a directory index.
// When nothing exists the joined path is returned as written.
func resolveJS(root, file, spec string) string {
	if spec != "." && spec != ".." && !strings.HasPrefix(spec, "./") && !strings.HasPrefix(spec, "../") {
		return ""
	}
	p, ok := inRepo(path.Join(path.Dir(file), spec))
	if !ok {
		return ""
	}
	if isFile(root, p) {
		return p
	}
	cands := []string{}
	for _, ext := range jsExts {
		cands = append(cands, p+ext)
	}
	if ext := path.Ext(p); ext == ".js" || ext == ".jsx" || ext == ".mjs" || ext == ".cjs" {
		stem := strings.TrimSuffix(p, ext)
		ts := map[string][]string{".js": {".ts", ".tsx"}, ".jsx": {".tsx"}, ".mjs": {".mts"}, ".cjs": {".cts"}}[ext]
		for _, e := range ts {
			cands = append(cands, stem+e)
		}
	}
	for _, ext := range jsExts {
		cands = append(cands, path.Join(p, "index"+ext))
	}
	for _, c := range cands {
		if isFile(root, c) {
			return c
		}
	}
	return p
}

// blankJSComments replaces // and /* */ comments with spaces, keeping
// newlines and string literals, so offsets and line numbers are unchanged.
// It also returns the [start, end) offsets of string literals, in order.
// Regex literals are not recognised.
func blankJSComments(src string) (string, [][2]int) {
	b := []byte(src)
	var strs [][2]int
	for i := 0; i < len(b); i++ {
		switch c := b[i]; {
		case c == '"' || c == '\'' || c == '`':
			start := i
			for i++; i < len(b) && b[i] != c; i++ {
				if b[i] == '\\' {
					i++
				} else if b[i] == '\n' && c != '`' {
					break // unterminated string
				}
			}
			strs = append(strs, [2]int{start, i + 1})
		case c == '/' && i+1 < len(b) && b[i+1] == '/':
			for ; i < len(b) && b[i] != '\n'; i++ {
				b[i] = ' '
			}
		case c == '/' && i+1 < len(b) && b[i+1] == '*':
			b[i], b[i+1] = ' ', ' '
			for i += 2; i < len(b); i++ {
				if b[i] == '*' && i+1 < len(b) && b[i+1] == '/' {
					b[i], b[i+1] = ' ', ' '
					i++
					break
				}
				if b[i] != '\n' {
					b[i] = ' '
				}
			}
		}
	}
	return string(b), strs
}
