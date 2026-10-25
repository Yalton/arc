package symbols

import (
	"regexp"
	"strings"
)

// jsExport matches a top-level named export declaration at column 0.
var jsExport = regexp.MustCompile(`^export\s+(?:default\s+)?(?:declare\s+)?(?:async\s+)?` +
	`(function\s*\*\s*|function\s+|abstract\s+class\s+|class\s+|const\s+enum\s+|const\s+|let\s+|var\s+|interface\s+|type\s+|enum\s+)` +
	`([A-Za-z_$][\w$]*)`)

// jsSymbols returns the named export declarations of a TS/JS file
// (function, class, const/let/var, interface, type, enum), each with the
// first sentence of the /** */ or // comment directly above it. Lines
// inside block comments and template literals are skipped.
func jsSymbols(file string, data []byte) []Symbol {
	var (
		out      []Symbol
		doc      []string // comment directly above the current line
		block    []string // lines of an open block comment
		inBlock  bool
		inTmpl   bool
		lastLine bool // previous line was a // comment
	)
	for i, line := range strings.Split(string(data), "\n") {
		t := strings.TrimSpace(line)
		switch {
		case inBlock:
			if j := strings.Index(line, "*/"); j >= 0 {
				inBlock, doc = false, append(block, line[:j])
				continue
			}
			block = append(block, line)
			continue
		case inTmpl:
			if j := strings.IndexByte(line, '`'); j >= 0 {
				inBlock, inTmpl = jsScan(line[j+1:])
			}
			doc, lastLine = nil, false
			continue
		case t == "":
			doc, lastLine = nil, false
			continue
		case strings.HasPrefix(t, "//"):
			if !lastLine {
				doc = nil
			}
			doc, lastLine = append(doc, strings.TrimPrefix(t, "//")), true
			continue
		case strings.HasPrefix(t, "/*"):
			body := strings.TrimPrefix(t, "/*")
			if j := strings.Index(body, "*/"); j >= 0 {
				doc = []string{body[:j]}
			} else {
				inBlock, block = true, []string{body}
			}
			lastLine = false
			continue
		case strings.HasPrefix(t, "@"):
			continue // decorator: the comment above still applies
		}
		if m := jsExport.FindStringSubmatch(line); m != nil {
			out = append(out, Symbol{Name: m[2], Kind: jsKind(m[1]), Doc: commentDoc(doc), File: file, Line: i + 1})
		}
		inBlock, inTmpl = jsScan(line)
		block, doc, lastLine = nil, nil, false
	}
	return out
}

func jsKind(kw string) string {
	f := strings.Fields(strings.TrimRight(kw, "* \t"))
	switch f[len(f)-1] {
	case "function":
		return KindFunc
	case "class":
		return KindClass
	case "let", "var":
		return KindVar
	case "interface", "type", "enum":
		return KindType
	}
	return KindConst
}

// jsScan reads one line of code and reports whether it ends inside a
// block comment or a template literal. Quoted strings and // comments are
// skipped; nested template expressions are not tracked.
func jsScan(line string) (inBlock, inTmpl bool) {
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case inBlock:
			if strings.HasPrefix(line[i:], "*/") {
				inBlock = false
				i++
			}
		case inTmpl:
			if c == '\\' {
				i++
			} else if c == '`' {
				inTmpl = false
			}
		case strings.HasPrefix(line[i:], "//"):
			return false, false
		case strings.HasPrefix(line[i:], "/*"):
			inBlock = true
			i++
		case c == '`':
			inTmpl = true
		case c == '\'' || c == '"':
			if end := stringEnd(line[i+1:], string(c)); end >= 0 {
				i += end + 1
			} else {
				i = len(line)
			}
		}
	}
	return inBlock, inTmpl
}

// commentDoc returns the first sentence of a comment's description: its
// lines with comment markers stripped, up to a blank line or a @tag.
func commentDoc(lines []string) string {
	var text []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		l = strings.TrimSpace(strings.TrimLeft(l, "*/"))
		if strings.HasPrefix(l, "@") || (l == "" && len(text) > 0) {
			break
		}
		if l != "" {
			text = append(text, l)
		}
	}
	return firstSentence(strings.Join(text, " "))
}
