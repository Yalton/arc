// Package brief answers "where does this file belong" (place) and "what do
// I need to know before touching this" (brief) from the registry (SPEC.md,
// "arc place" and "arc brief").
package brief

import (
	"path"
	"strings"

	"arc/internal/registry"
)

// Placement statuses.
const (
	StatusOwned   = "owned"
	StatusIgnored = "ignored"
	StatusUnowned = "unowned"
)

// MaxSuggestions is how many closest concepts place reports for an unowned path.
const MaxSuggestions = 3

// PlaceResult is the placement of one path.
type PlaceResult struct {
	Path    string       `json:"path"`
	Status  string       `json:"status"`
	Concept string       `json:"concept,omitempty"`
	Closest []Suggestion `json:"closest,omitempty"`
}

// Suggestion is a concept close to an unowned path. Prefix is the literal
// directory of the concept glob that matched best (may be empty).
type Suggestion struct {
	ID     string `json:"id"`
	Prefix string `json:"prefix,omitempty"`
}

// Place reports the owner of path (repo-relative, slash-separated). Ignore
// wins over ownership. For unowned paths, up to MaxSuggestions concepts are
// ranked by the number of leading path segments shared with any of the
// concept's glob literal prefixes, then by how many path segment tokens
// equal an alias, then registry order. Concepts scoring zero on both are
// left out, so fewer than three (or none) may be returned.
func Place(reg *registry.Registry, p string) PlaceResult {
	r := PlaceResult{Path: p}
	switch {
	case reg.IsIgnored(p):
		r.Status = StatusIgnored
	case reg.Owner(p) != nil:
		r.Status = StatusOwned
		r.Concept = reg.Owner(p).ID
	default:
		r.Status = StatusUnowned
		r.Closest = closest(reg, p)
	}
	return r
}

// PlaceMessage renders a result as one line, e.g.
// "internal/foo/x.go: unowned. Closest: goal (internal/workgraph), shared. Extend one, or add a concept to .arch/registry.yaml with a reason."
func PlaceMessage(r PlaceResult) string {
	switch r.Status {
	case StatusOwned:
		return r.Path + ": " + r.Concept
	case StatusIgnored:
		return r.Path + ": ignored"
	}
	const fix = "add a concept to " + registry.Path + " with a reason."
	if len(r.Closest) == 0 {
		return r.Path + ": unowned. No close concept. Add a concept to " + registry.Path + " with a reason."
	}
	names := make([]string, len(r.Closest))
	for i, s := range r.Closest {
		names[i] = s.ID
		if s.Prefix != "" {
			names[i] += " (" + s.Prefix + ")"
		}
	}
	return r.Path + ": unowned. Closest: " + strings.Join(names, ", ") + ". Extend one, or " + fix
}

type scored struct {
	s       Suggestion
	shared  int
	aliases int
}

func closest(reg *registry.Registry, p string) []Suggestion {
	segs := strings.Split(p, "/")
	var cands []scored
	for _, c := range reg.Concepts {
		best := scored{s: Suggestion{ID: c.ID}, shared: -1}
		for _, g := range c.Paths {
			lp := LiteralPrefix(g)
			if n := sharedSegments(segs, lp); n > best.shared {
				best.shared, best.s.Prefix = n, lp
			}
		}
		if best.shared < 0 {
			best.shared = 0
		}
		best.aliases = aliasMatches(segs, c.Aliases)
		if best.shared > 0 || best.aliases > 0 {
			cands = append(cands, best)
		}
	}
	// Stable insertion sort keeps registry order on ties.
	for i := 1; i < len(cands); i++ {
		for j := i; j > 0 && better(cands[j], cands[j-1]); j-- {
			cands[j], cands[j-1] = cands[j-1], cands[j]
		}
	}
	var out []Suggestion
	for i := 0; i < len(cands) && i < MaxSuggestions; i++ {
		out = append(out, cands[i].s)
	}
	return out
}

func better(a, b scored) bool {
	if a.shared != b.shared {
		return a.shared > b.shared
	}
	return a.aliases > b.aliases
}

// LiteralPrefix returns the directory part of a glob before its first
// metacharacter: "internal/workgraph/**" -> "internal/workgraph",
// "cmd/arc/main.go" -> "cmd/arc", "**/*.go" -> "".
func LiteralPrefix(glob string) string {
	if i := strings.IndexAny(glob, "*?[{\\"); i >= 0 {
		glob = glob[:i]
	}
	if i := strings.LastIndex(glob, "/"); i >= 0 {
		return glob[:i]
	}
	return ""
}

func sharedSegments(segs []string, prefix string) int {
	if prefix == "" {
		return 0
	}
	n := 0
	for i, ps := range strings.Split(prefix, "/") {
		if i >= len(segs) || segs[i] != ps {
			break
		}
		n++
	}
	return n
}

// aliasMatches counts path tokens equal (case-insensitively) to an alias.
// Tokens are path segments with the extension removed, split on '_', '-'
// and '.'.
func aliasMatches(segs []string, aliases []string) int {
	if len(aliases) == 0 {
		return 0
	}
	set := map[string]bool{}
	for _, a := range aliases {
		set[strings.ToLower(a)] = true
	}
	n := 0
	for _, s := range segs {
		s = strings.TrimSuffix(s, path.Ext(s))
		for _, tok := range strings.FieldsFunc(s, func(r rune) bool { return r == '_' || r == '-' || r == '.' }) {
			if set[strings.ToLower(tok)] {
				n++
			}
		}
	}
	return n
}
