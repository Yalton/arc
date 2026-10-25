// Package symbols extracts what each file already provides (its exported
// API) and searches it, so agents find existing code before writing new
// code (SPEC.md, "arc brief" and "arc find"). Go is read with go/parser;
// Python and TS/JS are scanned line by line.
package symbols

import (
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/Yalton/arc/internal/registry"
)

// Symbol kinds.
const (
	KindFunc   = "func"
	KindMethod = "method"
	KindType   = "type"
	KindClass  = "class"
	KindConst  = "const"
	KindVar    = "var"
)

// Symbol is one exported name. Methods are named Type.Method.
type Symbol struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	Doc  string `json:"doc,omitempty"` // first sentence of the doc comment
	File string `json:"file"`
	Line int    `json:"line"`
}

// maxDoc caps Doc so one long comment cannot crowd out the rest.
const maxDoc = 120

// Symbols reads files (repo-relative, slash-separated) under root and
// returns the exported symbols of each one in a supported language, in
// source order. Test files, files in other languages and files that no
// longer exist are absent; unparseable source yields what could be read.
func Symbols(root string, files []string) (map[string][]Symbol, error) {
	out := map[string][]Symbol{}
	for _, f := range files {
		parse := extractor(f)
		if parse == nil {
			continue
		}
		fp := filepath.Join(root, filepath.FromSlash(f))
		fi, err := os.Lstat(fp)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !fi.Mode().IsRegular() {
			continue
		}
		data, err := os.ReadFile(fp)
		if err != nil {
			return nil, err
		}
		if syms := parse(f, data); len(syms) > 0 {
			out[f] = syms
		}
	}
	return out, nil
}

// ByConcept returns the symbols of the non-ignored files each concept owns,
// keyed by concept id and sorted by file then line. Only concepts in ids
// are read; nil ids means all.
func ByConcept(reg *registry.Registry, root string, files []string, ids map[string]bool) (map[string][]Symbol, error) {
	owner := map[string]string{}
	var read []string
	for _, f := range files {
		if reg.IsIgnored(f) {
			continue
		}
		c := reg.Owner(f)
		if c == nil || (ids != nil && !ids[c.ID]) {
			continue
		}
		owner[f] = c.ID
		read = append(read, f)
	}
	per, err := Symbols(root, read)
	if err != nil {
		return nil, err
	}
	out := map[string][]Symbol{}
	for _, f := range read {
		out[owner[f]] = append(out[owner[f]], per[f]...)
	}
	for _, syms := range out {
		Sort(syms)
	}
	return out, nil
}

// Sort orders symbols by file, then line, then name.
func Sort(syms []Symbol) {
	sort.SliceStable(syms, func(i, j int) bool {
		a, b := syms[i], syms[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Name < b.Name
	})
}

// extractor returns the parser for file's language, or nil for test files
// and unsupported languages.
func extractor(file string) func(file string, data []byte) []Symbol {
	base := path.Base(file)
	switch ext := strings.ToLower(path.Ext(file)); ext {
	case ".go":
		if strings.HasSuffix(base, "_test.go") {
			return nil
		}
		return goSymbols
	case ".py":
		if strings.HasPrefix(base, "test_") || strings.HasSuffix(base, "_test.py") || base == "conftest.py" {
			return nil
		}
		return pythonSymbols
	case ".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs", ".mts", ".cts":
		stem := strings.TrimSuffix(base, path.Ext(base))
		if strings.HasSuffix(stem, ".test") || strings.HasSuffix(stem, ".spec") ||
			strings.Contains("/"+file, "/__tests__/") {
			return nil
		}
		return jsSymbols
	}
	return nil
}

// firstSentence collapses whitespace in a comment and keeps its first
// sentence, capped at maxDoc characters.
func firstSentence(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	for from := 0; ; {
		i := strings.Index(s[from:], ". ")
		if i < 0 {
			break
		}
		end := from + i + 1
		if !hasSuffix(s[:end], "e.g.", "i.e.", "etc.") {
			s = s[:end]
			break
		}
		from = end
	}
	if utf8.RuneCountInString(s) > maxDoc {
		r := []rune(s)
		s = strings.TrimRight(string(r[:maxDoc-3]), " ") + "..."
	}
	return s
}
