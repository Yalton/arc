package brief

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Yalton/arc/internal/registry"
	"github.com/Yalton/arc/internal/symbols"
)

// WholeTokenBudget is the target size of the no-argument brief; callers warn
// when Whole(...).Tokens() exceeds it.
const WholeTokenBudget = 4000

// MaxProvides caps the symbols Text lists per concept; JSON has them all.
const MaxProvides = 25

// wholeNote points from the whole-registry view, which omits provides, to
// the per-path brief that lists them.
const wholeNote = "Run 'arc brief <path>' for what the owning concept already provides, or 'arc find <words>' to search all of it."

// Brief is the agent-facing context for a scope. Entries keep registry order.
type Brief struct {
	System     string          `json:"system"`
	NonGoals   []NonGoalEntry  `json:"non_goals,omitempty"`
	Paths      []PlaceResult   `json:"paths,omitempty"`
	Concepts   []ConceptEntry  `json:"concepts,omitempty"`
	Boundaries []BoundaryEntry `json:"boundaries,omitempty"`
	Flags      []FlagEntry     `json:"flags,omitempty"`
	Removals   []RemovalEntry  `json:"removals,omitempty"`
	Note       string          `json:"note,omitempty"`
}

type NonGoalEntry struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// ConceptEntry is a concept as shown to agents. Paths is filled only in the
// whole-registry view; Provides only by AddProvides.
type ConceptEntry struct {
	ID      string   `json:"id"`
	Name    string   `json:"name,omitempty"`
	Summary string   `json:"summary,omitempty"`
	Paths   []string `json:"paths,omitempty"`
	Storage []string `json:"storage,omitempty"`
	Env     []string `json:"env,omitempty"`
	Rules   []string `json:"rules,omitempty"`
	Reason  string   `json:"reason,omitempty"`
	Draft   bool     `json:"draft,omitempty"`
	// Provides is the exported API of the concept's files (symbols.ByConcept).
	Provides []symbols.Symbol `json:"provides,omitempty"`
}

type BoundaryEntry struct {
	ID     string   `json:"id"`
	From   []string `json:"from"`
	Deny   []string `json:"deny"`
	Reason string   `json:"reason,omitempty"`
}

type FlagEntry struct {
	Name    string `json:"name"`
	Concept string `json:"concept,omitempty"`
	Purpose string `json:"purpose,omitempty"`
	Expires string `json:"expires,omitempty"`
	Bead    string `json:"bead,omitempty"`
}

type RemovalEntry struct {
	ID         string   `json:"id"`
	What       string   `json:"what,omitempty"`
	Paths      []string `json:"paths,omitempty"`
	ReplacedBy string   `json:"replaced_by,omitempty"`
	RemoveBy   string   `json:"remove_by,omitempty"`
	Bead       string   `json:"bead,omitempty"`
	Overdue    bool     `json:"overdue,omitempty"`
}

// ForPaths briefs the concepts owning paths. Each path is listed with its
// placement; boundaries are those whose from-globs match a path; flags are
// active ones of owning concepts; removals are those replaced by an owning
// concept or whose paths match a given path.
func ForPaths(reg *registry.Registry, paths []string, now time.Time) Brief {
	b := header(reg)
	ids := map[string]bool{}
	for _, p := range paths {
		r := Place(reg, p)
		b.Paths = append(b.Paths, r)
		if r.Status == StatusOwned {
			ids[r.Concept] = true
		}
	}
	b.Concepts = conceptEntries(reg, ids, false)
	for _, bd := range reg.Boundaries {
		if anyMatch(bd.From, paths) {
			b.Boundaries = append(b.Boundaries, boundaryEntry(bd))
		}
	}
	b.Flags = activeFlags(reg, ids, now)
	b.Removals = removals(reg, now, func(rm registry.Removal) bool {
		return ids[rm.ReplacedBy] || anyMatch(rm.Paths, paths)
	})
	return b
}

// ForConcepts briefs the given concept ids. Boundaries are those whose
// from-globs overlap a concept's paths; two globs overlap when one's literal
// prefix (LiteralPrefix) is a segment-prefix of the other's, so a glob with
// an empty prefix such as "**/*.go" overlaps everything. Removals are those
// replaced by a listed concept or whose paths overlap its paths.
func ForConcepts(reg *registry.Registry, ids []string, now time.Time) (Brief, error) {
	set := map[string]bool{}
	var globs []string
	for _, id := range ids {
		c := reg.Concept(id)
		if c == nil {
			return Brief{}, fmt.Errorf("unknown concept %q", id)
		}
		set[id] = true
		globs = append(globs, c.Paths...)
	}
	b := header(reg)
	b.Concepts = conceptEntries(reg, set, false)
	for _, bd := range reg.Boundaries {
		if anyOverlap(bd.From, globs) {
			b.Boundaries = append(b.Boundaries, boundaryEntry(bd))
		}
	}
	b.Flags = activeFlags(reg, set, now)
	b.Removals = removals(reg, now, func(rm registry.Removal) bool {
		return set[rm.ReplacedBy] || anyOverlap(rm.Paths, globs)
	})
	return b, nil
}

// ConceptIDs returns the ids of the briefed concepts, as a set.
func (b Brief) ConceptIDs() map[string]bool {
	ids := map[string]bool{}
	for _, c := range b.Concepts {
		ids[c.ID] = true
	}
	return ids
}

// AddProvides attaches each concept's symbols (keyed by concept id, as
// symbols.ByConcept returns them) to its entry.
func (b *Brief) AddProvides(by map[string][]symbols.Symbol) {
	for i := range b.Concepts {
		b.Concepts[i].Provides = by[b.Concepts[i].ID]
	}
}

// Whole is the compact always-loaded view of the entire registry: every
// concept with its paths, every boundary, active flags and all removals.
// It omits provides to stay within WholeTokenBudget; Note points to the
// per-path brief instead.
func Whole(reg *registry.Registry, now time.Time) Brief {
	b := header(reg)
	b.Note = wholeNote
	all := map[string]bool{}
	for _, c := range reg.Concepts {
		all[c.ID] = true
	}
	b.Concepts = conceptEntries(reg, all, true)
	for _, bd := range reg.Boundaries {
		b.Boundaries = append(b.Boundaries, boundaryEntry(bd))
	}
	b.Flags = activeFlags(reg, nil, now)
	b.Removals = removals(reg, now, func(registry.Removal) bool { return true })
	return b
}

// Tokens estimates the size of Text() as chars/4.
func (b Brief) Tokens() int { return len(b.Text()) / 4 }

// Text renders the brief as compact plain text for agents.
func (b Brief) Text() string {
	var w strings.Builder
	fmt.Fprintf(&w, "System: %s\n", b.System)
	if len(b.NonGoals) > 0 {
		w.WriteString("Non-goals:\n")
		for _, n := range b.NonGoals {
			fmt.Fprintf(&w, "- %s: %s\n", n.ID, n.Text)
		}
	}
	if len(b.Paths) > 0 {
		w.WriteString("Paths:\n")
		for _, p := range b.Paths {
			fmt.Fprintf(&w, "- %s\n", PlaceMessage(p))
		}
	}
	for _, c := range b.Concepts {
		writeConcept(&w, c)
	}
	if len(b.Boundaries) > 0 {
		w.WriteString("Boundaries:\n")
		for _, bd := range b.Boundaries {
			fmt.Fprintf(&w, "- %s: %s must not import %s.", bd.ID, strings.Join(bd.From, ", "), strings.Join(bd.Deny, ", "))
			if bd.Reason != "" {
				w.WriteString(" " + bd.Reason)
			}
			w.WriteString("\n")
		}
	}
	if len(b.Flags) > 0 {
		w.WriteString("Flags:\n")
		for _, f := range b.Flags {
			exp := ""
			if f.Expires != "" {
				exp = "expires " + f.Expires
			}
			fmt.Fprintf(&w, "- %s (%s): %s\n", f.Name, joinNonEmpty(f.Concept, exp, f.Bead), f.Purpose)
		}
	}
	if len(b.Removals) > 0 {
		w.WriteString("Removals:\n")
		for _, r := range b.Removals {
			by := "remove by " + r.RemoveBy
			if r.Overdue {
				by += " OVERDUE"
			}
			repl := ""
			if r.ReplacedBy != "" {
				repl = "replaced by " + r.ReplacedBy
			}
			fmt.Fprintf(&w, "- %s: %s (%s)\n", r.ID, r.What, joinNonEmpty(repl, by, r.Bead))
		}
	}
	if b.Note != "" {
		w.WriteString(b.Note + "\n")
	}
	return w.String()
}

// ProvidesText renders only what each concept provides, for context about
// a concept already briefed.
func (b Brief) ProvidesText() string {
	var w strings.Builder
	for _, c := range b.Concepts {
		if len(c.Provides) > 0 {
			fmt.Fprintf(&w, "Concept %s", c.ID)
			writeProvides(&w, c.Provides)
		}
	}
	return w.String()
}

// writeProvides lists up to MaxProvides symbol names grouped by file.
// When it has to cut, it takes names from each file in turn (in source
// order), top-level names before methods, so every file stays visible.
func writeProvides(w *strings.Builder, syms []symbols.Symbol) {
	shown := pickProvides(syms, MaxProvides)
	more := len(syms) - len(shown)
	byFile := map[string][]string{}
	var files []string
	for _, s := range shown {
		if byFile[s.File] == nil {
			files = append(files, s.File)
		}
		byFile[s.File] = append(byFile[s.File], s.Name)
	}
	sort.Strings(files)
	w.WriteString(" provides (extend these before adding new code):\n")
	for _, f := range files {
		names := byFile[f]
		sort.Strings(names)
		fmt.Fprintf(w, "    %s: %s\n", f, strings.Join(names, ", "))
	}
	if more > 0 {
		fmt.Fprintf(w, "    +%d more; arc brief --json lists all\n", more)
	}
}

// pickProvides returns up to max of syms, round-robin over files in path
// order: first top-level names, then methods.
func pickProvides(syms []symbols.Symbol, max int) []symbols.Symbol {
	if len(syms) <= max {
		return syms
	}
	sorted := append([]symbols.Symbol(nil), syms...)
	symbols.Sort(sorted)
	var out []symbols.Symbol
	for _, methods := range []bool{false, true} {
		queues := map[string][]symbols.Symbol{}
		var files []string
		for _, s := range sorted {
			if (s.Kind == symbols.KindMethod) != methods {
				continue
			}
			if queues[s.File] == nil {
				files = append(files, s.File)
			}
			queues[s.File] = append(queues[s.File], s)
		}
		for left := true; left; {
			left = false
			for _, f := range files {
				if len(out) == max {
					return out
				}
				if q := queues[f]; len(q) > 0 {
					out, queues[f], left = append(out, q[0]), q[1:], true
				}
			}
		}
	}
	return out
}

func writeConcept(w *strings.Builder, c ConceptEntry) {
	fmt.Fprintf(w, "Concept %s", c.ID)
	if c.Name != "" {
		fmt.Fprintf(w, " (%s)", c.Name)
	}
	if c.Draft {
		w.WriteString(" [draft]")
	}
	if c.Summary != "" {
		w.WriteString(": " + oneLine(c.Summary))
	}
	w.WriteString("\n")
	if c.Reason != "" {
		w.WriteString("  why: " + c.Reason + "\n")
	}
	for _, kv := range []struct {
		k string
		v []string
	}{{"paths", c.Paths}, {"storage", c.Storage}, {"env", c.Env}} {
		if len(kv.v) > 0 {
			fmt.Fprintf(w, "  %s: %s\n", kv.k, strings.Join(kv.v, ", "))
		}
	}
	for _, r := range c.Rules {
		fmt.Fprintf(w, "  - %s\n", oneLine(r))
	}
	if len(c.Provides) > 0 {
		w.WriteString(" ")
		writeProvides(w, c.Provides)
	}
}

func header(reg *registry.Registry) Brief {
	b := Brief{System: reg.System.Name}
	if s := firstLine(reg.System.Summary); s != "" {
		if b.System != "" {
			b.System += ": "
		}
		b.System += s
	}
	for _, n := range reg.NonGoals {
		b.NonGoals = append(b.NonGoals, NonGoalEntry{ID: n.ID, Text: oneLine(n.Text)})
	}
	return b
}

func conceptEntries(reg *registry.Registry, ids map[string]bool, withPaths bool) []ConceptEntry {
	var out []ConceptEntry
	for _, c := range reg.Concepts {
		if !ids[c.ID] {
			continue
		}
		e := ConceptEntry{ID: c.ID, Name: c.Name, Summary: oneLine(c.Summary), Storage: c.Storage, Env: c.Env, Rules: c.Rules, Reason: oneLine(c.Reason), Draft: c.Draft}
		if withPaths {
			e.Paths = c.Paths
		}
		out = append(out, e)
	}
	return out
}

func boundaryEntry(bd registry.Boundary) BoundaryEntry {
	return BoundaryEntry{ID: bd.ID, From: bd.From, Deny: bd.Deny, Reason: oneLine(bd.Reason)}
}

// activeFlags returns flags of the given concepts (all when ids is nil) that
// have not expired; a flag is active through its expiry date (UTC).
// Unparseable dates are kept so validation reports them.
func activeFlags(reg *registry.Registry, ids map[string]bool, now time.Time) []FlagEntry {
	var out []FlagEntry
	for _, f := range reg.Flags {
		if ids != nil && !ids[f.Concept] {
			continue
		}
		if t, err := f.ExpiresAt(); err == nil && pastDay(now, t) {
			continue
		}
		out = append(out, FlagEntry{Name: f.Name, Concept: f.Concept, Purpose: oneLine(f.Purpose), Expires: f.Expires, Bead: f.Bead})
	}
	return out
}

// removals returns pending removals selected by keep; ones whose remove_by
// day has passed are marked Overdue (they stay listed until the entry is
// deleted from the registry).
func removals(reg *registry.Registry, now time.Time, keep func(registry.Removal) bool) []RemovalEntry {
	var out []RemovalEntry
	for _, rm := range reg.Removals {
		if !keep(rm) {
			continue
		}
		e := RemovalEntry{ID: rm.ID, What: oneLine(rm.What), Paths: rm.Paths, ReplacedBy: rm.ReplacedBy, RemoveBy: rm.RemoveBy, Bead: rm.Bead}
		if t, err := rm.RemoveByAt(); err == nil && pastDay(now, t) {
			e.Overdue = true
		}
		out = append(out, e)
	}
	return out
}

// pastDay reports whether now is after the whole UTC day starting at day.
func pastDay(now, day time.Time) bool { return !now.Before(day.AddDate(0, 0, 1)) }

func anyMatch(globs, paths []string) bool {
	for _, g := range globs {
		for _, p := range paths {
			if registry.Match(g, p) {
				return true
			}
		}
	}
	return false
}

func anyOverlap(a, b []string) bool {
	for _, x := range a {
		for _, y := range b {
			if overlaps(x, y) {
				return true
			}
		}
	}
	return false
}

func overlaps(a, b string) bool {
	pa, pb := LiteralPrefix(a), LiteralPrefix(b)
	return segPrefix(pa, pb) || segPrefix(pb, pa)
}

func segPrefix(prefix, s string) bool {
	return prefix == "" || s == prefix || strings.HasPrefix(s, prefix+"/")
}

// firstLine returns the summary's first sentence, with line breaks
// collapsed, so a wrapped YAML block does not cut off mid-sentence.
func firstLine(s string) string {
	s = oneLine(s)
	if i := strings.Index(s, ". "); i >= 0 {
		s = s[:i+1]
	}
	return s
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func joinNonEmpty(parts ...string) string {
	var out []string
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, ", ")
}
