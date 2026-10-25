package symbols

import (
	"regexp"
	"strings"
)

// pyDef matches a top-level def or class at column 0.
var pyDef = regexp.MustCompile(`^(?:async[ \t]+)?(def|class)[ \t]+([A-Za-z_][A-Za-z0-9_]*)`)

// pythonSymbols returns the top-level functions and classes of a Python
// file whose names do not start with an underscore, with the first line
// of their docstring. Text inside string literals never counts as a
// definition.
func pythonSymbols(file string, data []byte) []Symbol {
	lines := strings.Split(string(data), "\n")
	var out []Symbol
	open := "" // closing delimiter of a string continued from the previous line
	for i := 0; i < len(lines); i++ {
		inString := open != ""
		code, o := pyCode(lines[i], open)
		open = o
		m := pyDef.FindStringSubmatch(code)
		if inString || m == nil {
			continue
		}
		// The signature ends on the line where brackets balance again.
		end, depth := i, bracketDelta(code)
		for depth > 0 && end+1 < len(lines) {
			end++
			code, open = pyCode(lines[end], open)
			depth += bracketDelta(code)
		}
		if m[2][0] == '_' {
			i = end
			continue
		}
		kind := KindFunc
		if m[1] == "class" {
			kind = KindClass
		}
		s := Symbol{Name: m[2], Kind: kind, File: file, Line: i + 1}
		if c := strings.TrimSpace(code); strings.HasSuffix(c, ":") {
			s.Doc = pyDocstring(lines[end+1:])
		}
		out = append(out, s)
		i = end
	}
	return out
}

// pyDocstring returns the first sentence of a docstring opening the body
// whose lines are given, or "".
func pyDocstring(body []string) string {
	for k, l := range body {
		t := strings.TrimSpace(l)
		if t == "" {
			continue
		}
		t = strings.TrimLeft(t, "rRuUbB")
		q := ""
		for _, d := range []string{`"""`, `'''`, `"`, `'`} {
			if strings.HasPrefix(t, d) {
				q = d
				break
			}
		}
		if q == "" {
			return ""
		}
		var text []string
		rest := t[len(q):]
		for _, next := range append([]string{rest}, body[k+1:]...) {
			if i := strings.Index(next, q); i >= 0 {
				text = append(text, next[:i])
				break
			}
			text = append(text, next)
			if len(q) == 1 || strings.Contains(strings.Join(text, " "), ". ") {
				break
			}
		}
		return firstSentence(strings.Join(text, " "))
	}
	return ""
}

// pyCode blanks comments and string literal contents in one line. open is
// the closing delimiter of a string continued from the previous line; the
// returned delimiter is that of a triple-quoted string left open at the
// end of this line.
func pyCode(line, open string) (code, stillOpen string) {
	if open != "" {
		j := strings.Index(line, open)
		if j < 0 {
			return "", open
		}
		line = line[j+len(open):]
	}
	var b strings.Builder
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch c {
		case '#':
			return b.String(), ""
		case '\'', '"':
			q := string(c)
			if strings.HasPrefix(line[i:], q+q+q) {
				q += q + q
			}
			end := stringEnd(line[i+len(q):], q)
			if end < 0 {
				if len(q) == 3 {
					return b.String(), q
				}
				return b.String(), "" // unterminated: ends with the line
			}
			b.WriteString(`""`)
			i += len(q) + end + len(q) - 1
			continue
		}
		b.WriteByte(c)
	}
	return b.String(), ""
}

// stringEnd returns the index in s of closing delimiter q, skipping
// backslash escapes, or -1.
func stringEnd(s, q string) int {
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

// bracketDelta is the net change in bracket depth over code.
func bracketDelta(code string) int {
	d := 0
	for i := 0; i < len(code); i++ {
		switch code[i] {
		case '(', '[', '{':
			d++
		case ')', ']', '}':
			d--
		}
	}
	return d
}
