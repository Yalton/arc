package symbols

import (
	"sort"
	"strings"
	"unicode"

	"github.com/Yalton/arc/internal/registry"
)

// Result limits for Find.
const (
	MaxConceptMatches = 3
	MaxSymbolMatches  = 10
)

// Match weights: a query word counts once, at the best field it matches.
// An exact token match scores the full weight, a prefix match (the shorter
// token at least 4 letters) half of it.
const (
	weightName = 6 // symbol name; concept id, name or alias
	weightDoc  = 3 // symbol doc
	weightText = 2 // concept summary, reason or rules
	weightPath = 2 // symbol file path segments
)

// FindResult is the answer to 'arc find'.
type FindResult struct {
	Query    []string       `json:"query"` // query words used, stopwords dropped
	Concepts []ConceptMatch `json:"concepts"`
	Symbols  []SymbolMatch  `json:"symbols"`
}

// ConceptMatch is a concept matching the query; Why lists each matched
// query word with the field it matched ("label (alias)").
type ConceptMatch struct {
	ID      string   `json:"id"`
	Name    string   `json:"name,omitempty"`
	Summary string   `json:"summary,omitempty"`
	Score   int      `json:"score"`
	Why     []string `json:"why"`
}

// SymbolMatch is a symbol matching the query, with its owning concept.
type SymbolMatch struct {
	Symbol
	Concept string `json:"concept"`
	Score   int    `json:"score"`
}

// Find ranks the concepts of reg and the symbols in provides (keyed by
// concept id, as ByConcept returns them) against the words of query.
// Concepts are ordered by score then registry order; symbols by score,
// then shorter names, then file and line. Symbols scoring under a third
// of the best one are dropped as noise.
func Find(reg *registry.Registry, provides map[string][]Symbol, query string) FindResult {
	var q, orig []string // stemmed query words and the words as typed
	for _, w := range words(query) {
		if st := stem(w); !contains(q, st) {
			q, orig = append(q, st), append(orig, w)
		}
	}
	res := FindResult{Query: orig, Concepts: []ConceptMatch{}, Symbols: []SymbolMatch{}}
	if len(q) == 0 {
		return res
	}
	for _, c := range reg.Concepts {
		fields := []field{
			{"id", Tokens(c.ID), weightName},
			{"name", Tokens(c.Name), weightName},
			{"alias", Tokens(strings.Join(c.Aliases, " ")), weightName},
			{"summary", Tokens(c.Summary), weightText},
			{"reason", Tokens(c.Reason), weightText},
			{"rules", Tokens(strings.Join(c.Rules, " ")), weightText},
		}
		score, why := scoreFields(q, orig, fields)
		if score > 0 {
			res.Concepts = append(res.Concepts, ConceptMatch{ID: c.ID, Name: c.Name, Summary: oneLine(c.Summary), Score: score, Why: why})
		}
	}
	sort.SliceStable(res.Concepts, func(i, j int) bool { return res.Concepts[i].Score > res.Concepts[j].Score })
	if len(res.Concepts) > MaxConceptMatches {
		res.Concepts = res.Concepts[:MaxConceptMatches]
	}

	var syms []SymbolMatch
	for _, c := range reg.Concepts {
		for _, s := range provides[c.ID] {
			fields := []field{
				{"name", Tokens(s.Name), weightName},
				{"doc", Tokens(s.Doc), weightDoc},
				{"path", Tokens(strings.TrimSuffix(s.File, pathExt(s.File))), weightPath},
			}
			if score, _ := scoreFields(q, orig, fields); score > 0 {
				syms = append(syms, SymbolMatch{Symbol: s, Concept: c.ID, Score: score})
			}
		}
	}
	sort.SliceStable(syms, func(i, j int) bool {
		a, b := syms[i], syms[j]
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		if la, lb := len(Tokens(a.Name)), len(Tokens(b.Name)); la != lb {
			return la < lb
		}
		if a.File != b.File {
			return a.File < b.File
		}
		return a.Line < b.Line
	})
	for _, s := range syms {
		if len(res.Symbols) == MaxSymbolMatches || s.Score*3 < syms[0].Score {
			break
		}
		res.Symbols = append(res.Symbols, s)
	}
	return res
}

// Covered returns, in Find order, symbols whose name (for a method, the
// part after the receiver type) has at least two words, all of which
// appear in text: strong evidence that text
// describes something that already exists (used by triage). At most max
// are returned.
func Covered(reg *registry.Registry, provides map[string][]Symbol, text string, max int) []SymbolMatch {
	words := map[string]bool{}
	for _, w := range Tokens(text) {
		words[w] = true
	}
	var out []SymbolMatch
	for _, m := range Find(reg, provides, text).Symbols {
		n := m.Name
		if i := strings.LastIndexByte(n, '.'); i >= 0 {
			n = n[i+1:] // a method's receiver type is not part of what it does
		}
		name := dedupe(Tokens(n))
		ok := len(name) >= 2
		for _, w := range name {
			ok = ok && words[w]
		}
		if ok {
			out = append(out, m)
			if len(out) == max {
				break
			}
		}
	}
	return out
}

type field struct {
	name   string
	tokens []string
	weight int
}

// scoreFields sums, over stemmed query words q, the best weight a field
// gives each, and says which field each matched word (as typed, orig) hit.
func scoreFields(q, orig []string, fields []field) (int, []string) {
	total := 0
	var why []string
	for i, w := range q {
		best, where := 0, ""
		for _, f := range fields {
			if s := f.weight * tokenMatch(w, f.tokens) / 2; s > best {
				best, where = s, f.name
			}
		}
		if best > 0 {
			total += best
			why = append(why, orig[i]+" ("+where+")")
		}
	}
	return total, why
}

// tokenMatch is 2 when w equals a token, 1 when one is a prefix of the
// other and the shorter has at least 4 letters, else 0.
func tokenMatch(w string, tokens []string) int {
	best := 0
	for _, t := range tokens {
		if t == w {
			return 2
		}
		short, long := w, t
		if len(short) > len(long) {
			short, long = long, short
		}
		if len(short) >= 4 && strings.HasPrefix(long, short) {
			best = 1
		}
	}
	return best
}

// Tokens splits s into lowercase, stemmed words: on non-alphanumerics, at
// camelCase boundaries (so "parseHTTPLine" and
// "parse_http_line" agree). Stopwords and one-letter words are dropped.
func Tokens(s string) []string {
	ws := words(s)
	for i, w := range ws {
		ws[i] = stem(w)
	}
	return ws
}

// words splits s as Tokens does, without stemming.
func words(s string) []string {
	var out []string
	add := func(w string) {
		w = strings.ToLower(w)
		if len(w) >= 2 && !stopwords[w] {
			out = append(out, w)
		}
	}
	r := []rune(s)
	start := -1
	for i := 0; i <= len(r); i++ {
		if i < len(r) && (unicode.IsLetter(r[i]) || unicode.IsDigit(r[i])) {
			if start < 0 {
				start = i
			} else if boundary(r, i) {
				add(string(r[start:i]))
				start = i
			}
			continue
		}
		if start >= 0 {
			add(string(r[start:i]))
			start = -1
		}
	}
	return out
}

// boundary reports whether a word starts at r[i] inside a run of letters
// and digits: lower to upper ("parseLine") or the last capital of an
// acronym before a lowercase letter ("HTTPServer"). Digits stay attached
// ("base64", "v2").
func boundary(r []rune, i int) bool {
	prev, cur := r[i-1], r[i]
	switch {
	case (unicode.IsLower(prev) || unicode.IsDigit(prev)) && unicode.IsUpper(cur):
		return true
	case unicode.IsUpper(prev) && unicode.IsUpper(cur) && i+1 < len(r) && unicode.IsLower(r[i+1]):
		return true
	}
	return false
}

// stem strips common English inflections so "labels", "parsing" and
// "parsed" meet "label" and "parse". It is deliberately crude; both sides
// of a comparison go through it.
func stem(w string) string {
	if len(w) <= 3 {
		return w
	}
	switch {
	case strings.HasSuffix(w, "ies") && len(w) > 4:
		w = w[:len(w)-3] + "y"
	case hasSuffix(w, "sses", "ches", "shes", "xes", "zes"):
		w = w[:len(w)-2]
	case hasSuffix(w, "ss", "us", "is"):
	case strings.HasSuffix(w, "s"):
		w = w[:len(w)-1]
	}
	for _, suf := range []string{"ing", "ed"} {
		if strings.HasSuffix(w, suf) && len(w)-len(suf) >= 3 {
			w = w[:len(w)-len(suf)]
			// "mapped" -> "map", but keep "call", "pass", "buzz".
			if n := len(w); w[n-1] == w[n-2] && !strings.ContainsRune("lsz", rune(w[n-1])) {
				w = w[:n-1]
			}
			break
		}
	}
	if len(w) > 3 && strings.HasSuffix(w, "e") {
		w = w[:len(w)-1]
	}
	return w
}

func hasSuffix(w string, sufs ...string) bool {
	for _, s := range sufs {
		if strings.HasSuffix(w, s) {
			return true
		}
	}
	return false
}

// stopwords are dropped from queries and names: they say nothing about
// what code does.
var stopwords = func() map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(`a an and are as at be by can do does for from has have how i
		if in into is it its my need of on or our should so some that the their them then there
		these this to use uses using want we what when where which while who will with would you`) {
		m[w] = true
	}
	return m
}()

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

func dedupe(ws []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, w := range ws {
		if !seen[w] {
			seen[w] = true
			out = append(out, w)
		}
	}
	return out
}

func pathExt(p string) string {
	if i := strings.LastIndexByte(p, '.'); i > strings.LastIndexByte(p, '/') {
		return p[i:]
	}
	return ""
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }
