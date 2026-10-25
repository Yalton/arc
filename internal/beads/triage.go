package beads

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/Yalton/arc/internal/brief"
	"github.com/Yalton/arc/internal/registry"
	"github.com/Yalton/arc/internal/symbols"
)

// Labels triage adds (SPEC.md, "Beads integration details").
const (
	LabelOK     = "arch:ok"
	LabelReview = "arch:review"
	LabelPrefix = "arch:"
)

// TemplateFields must appear in a bead, as a heading or a "Field:" line.
var TemplateFields = []string{"Replaces / removes", "Concepts touched", "Closest existing thing"}

// Notes written by triage are wrapped in these lines so later runs can
// leave them out of the analysed text and skip notes with the same
// decision.
const (
	noteBegin = "--- arc triage ---"
	noteEnd   = "--- end arc triage ---"
)

var arcNote = regexp.MustCompile(`(?s)` + regexp.QuoteMeta(noteBegin) + `.*?` + regexp.QuoteMeta(noteEnd))

// NonGoalHit is a registry non-goal whose keyword appears in a bead.
type NonGoalHit struct {
	ID      string `json:"id"`
	Keyword string `json:"keyword"`
}

// Result is the triage of one bead.
type Result struct {
	ID              string       `json:"id"`
	Title           string       `json:"title,omitempty"`
	Label           string       `json:"label,omitempty"`
	MissingFields   []string     `json:"missing_fields,omitempty"`
	Concepts        []string     `json:"concepts,omitempty"`
	UnknownConcepts []string     `json:"unknown_concepts,omitempty"`
	NonGoals        []NonGoalHit `json:"non_goals,omitempty"`
	Reasons         []string     `json:"reasons,omitempty"`
	Note            string       `json:"note,omitempty"` // the note triage appends
	NoteAdded       bool         `json:"note_added"`
	LabelAdded      bool         `json:"label_added"`
	LabelRemoved    string       `json:"label_removed,omitempty"` // the opposite arch:ok/arch:review label triage removed
	Error           string       `json:"error,omitempty"`
}

// Summary renders the result as one line for people and hook context.
func (r Result) Summary() string {
	switch {
	case r.Error != "":
		return r.ID + ": error: " + r.Error
	case r.Label == LabelOK:
		return fmt.Sprintf("%s: %s (concepts: %s)", r.ID, r.Label, strings.Join(r.Concepts, ", "))
	}
	return r.ID + ": " + r.Label + ": " + strings.Join(r.Reasons, "; ")
}

// Text is the bead text triage analyses: title, description, design,
// acceptance criteria and notes, without notes arc wrote itself.
func Text(b Bead) string {
	notes := arcNote.ReplaceAllString(b.Notes, "")
	return strings.Join([]string{b.Title, b.Description, b.Design, b.AcceptanceCriteria, notes}, "\n\n")
}

// maxCovered is how many existing symbols a review reason names.
const maxCovered = 3

// Analyze triages b against reg without touching beads. provides is what
// each concept exports (symbols.ByConcept; nil skips the existing-code
// check). The returned Note is what Triage would append.
func Analyze(reg *registry.Registry, provides map[string][]symbols.Symbol, b Bead, now time.Time) Result {
	text := Text(b)
	r := Result{ID: b.ID, Title: b.Title}
	for _, f := range TemplateFields {
		if fieldLine(f).FindStringIndex(text) == nil {
			r.MissingFields = append(r.MissingFields, f)
			r.Reasons = append(r.Reasons, "missing template field: "+f)
		}
	}
	r.Concepts, r.UnknownConcepts = beadConcepts(reg, text)
	for _, e := range r.UnknownConcepts {
		r.Reasons = append(r.Reasons, fmt.Sprintf(
			"'Concepts touched' names %q, which is not in the registry; list registry concept ids separated by commas, or add the concept to %s with a reason",
			e, registry.Path))
	}
	if len(r.Concepts) == 0 {
		r.Reasons = append(r.Reasons, "no registry concept resolved from the bead text; name concept ids, aliases or repo paths")
	}
	if closest := fieldValue(text, "Closest existing thing"); len(closest) == 0 || isNone(cleanEntry(strings.Join(closest, " "))) {
		if hits := symbols.Covered(reg, provides, withoutFieldNames(b.Title+"\n"+b.Description), maxCovered); len(hits) > 0 {
			var names []string
			for _, h := range hits {
				names = append(names, fmt.Sprintf("%s:%d %s", h.File, h.Line, h.Name))
			}
			r.Reasons = append(r.Reasons, "existing code may already cover this: "+strings.Join(names, ", ")+
				"; extend it or say why not under 'Closest existing thing'")
		}
	}
	for _, n := range reg.NonGoals {
		for _, k := range n.Keywords {
			if mentions(text, k) {
				r.NonGoals = append(r.NonGoals, NonGoalHit{ID: n.ID, Keyword: k})
				r.Reasons = append(r.Reasons, fmt.Sprintf("matches non-goal %s (keyword %q): %s", n.ID, k, strings.Join(strings.Fields(n.Text), " ")))
				break
			}
		}
	}
	r.Label = LabelOK
	if len(r.Reasons) > 0 {
		r.Label = LabelReview
	}
	r.Note = note(reg, r, now)
	return r
}

// Concepts returns the concepts triage resolves for b, in registry order.
func Concepts(reg *registry.Registry, b Bead) []string {
	ids, _ := beadConcepts(reg, Text(b))
	return ids
}

// beadConcepts resolves concepts from text (ResolveConcepts) plus the
// entries of its 'Concepts touched' field; entries naming no registry
// concept are returned as unknown.
func beadConcepts(reg *registry.Registry, text string) (ids, unknown []string) {
	resolved := map[string]bool{}
	for _, id := range ResolveConcepts(reg, text) {
		resolved[id] = true
	}
	for _, e := range conceptsTouched(text) {
		if id := conceptFor(reg, e); id != "" {
			resolved[id] = true
		} else {
			unknown = append(unknown, e)
		}
	}
	for _, c := range reg.Concepts {
		if resolved[c.ID] {
			ids = append(ids, c.ID)
		}
	}
	return ids, unknown
}

func note(reg *registry.Registry, r Result, now time.Time) string {
	var w strings.Builder
	w.WriteString(noteBegin + "\n" + r.Label + "\n")
	for _, reason := range r.Reasons {
		w.WriteString("- " + reason + "\n")
	}
	if len(r.Concepts) > 0 {
		if b, err := brief.ForConcepts(reg, r.Concepts, now); err == nil {
			w.WriteString(noteBrief + b.Text())
		}
	}
	w.WriteString(noteEnd)
	return w.String()
}

// noteBrief starts the brief part of a triage note, which depends on the
// date; the part before it (label and reasons) is the decision.
const noteBrief = "arc brief:\n"

// noteDecision returns the label and reasons part of a triage note.
func noteDecision(n string) string {
	n = strings.TrimSuffix(n, noteEnd)
	if i := strings.Index(n, noteBrief); i >= 0 {
		n = n[:i]
	}
	return n
}

// sameDecision reports whether the most recent triage note in notes has
// the same label and reasons as note.
func sameDecision(notes, note string) bool {
	all := arcNote.FindAllString(notes, -1)
	return len(all) > 0 && noteDecision(all[len(all)-1]) == noteDecision(note)
}

// Triage analyses b and, unless dryRun, appends the note (skipped when the
// most recent triage note has the same label and reasons, since the brief
// in it changes with the date), adds the label (skipped when present) and
// removes the opposite one of arch:ok and arch:review when present. It
// never touches other labels.
func Triage(c *Client, reg *registry.Registry, provides map[string][]symbols.Symbol, b Bead, now time.Time, dryRun bool) (Result, error) {
	r := Analyze(reg, provides, b, now)
	if dryRun {
		return r, nil
	}
	if !sameDecision(b.Notes, r.Note) {
		if err := c.Note(b.ID, r.Note); err != nil {
			return r, err
		}
		r.NoteAdded = true
	}
	if !b.HasLabel(r.Label) {
		if err := c.AddLabel(b.ID, r.Label); err != nil {
			return r, err
		}
		r.LabelAdded = true
	}
	if opp := opposite(r.Label); b.HasLabel(opp) {
		if err := c.RemoveLabel(b.ID, opp); err != nil {
			return r, err
		}
		r.LabelRemoved = opp
	}
	return r, nil
}

// opposite returns the triage label that label replaces.
func opposite(label string) string {
	if label == LabelOK {
		return LabelReview
	}
	return LabelOK
}

// ResolveConcepts returns, in registry order, the concepts text refers to
// by id or alias (whole words, case-insensitive) or by a repo path that a
// concept owns.
func ResolveConcepts(reg *registry.Registry, text string) []string {
	found := map[string]bool{}
	for _, p := range pathToken.FindAllString(text, -1) {
		if id := ownerOfPath(reg, p); id != "" {
			found[id] = true
		}
	}
	var out []string
	for _, c := range reg.Concepts {
		hit := found[c.ID] || mentions(text, c.ID)
		for _, a := range c.Aliases {
			hit = hit || mentions(text, a)
		}
		if hit {
			out = append(out, c.ID)
		}
	}
	return out
}

// pathToken matches slash-separated path-like words such as
// "internal/goal" or "cmd/arc/main.go".
var pathToken = regexp.MustCompile(`[A-Za-z0-9_.-]+(?:/[A-Za-z0-9_.*-]+)+/?`)

func ownerOfPath(reg *registry.Registry, p string) string {
	if i := strings.IndexAny(p, "*?["); i >= 0 {
		p = p[:i]
	}
	p = strings.Trim(strings.TrimPrefix(p, "./"), "/.")
	if p == "" {
		return ""
	}
	if r := brief.Place(reg, p); r.Status == brief.StatusOwned {
		return r.Concept
	}
	return ""
}

// conceptFor maps one 'Concepts touched' entry to a concept id by id,
// name, alias (case-insensitive) or owned path; "" when none matches.
func conceptFor(reg *registry.Registry, entry string) string {
	for _, c := range reg.Concepts {
		if strings.EqualFold(entry, c.ID) || strings.EqualFold(entry, c.Name) {
			return c.ID
		}
		for _, a := range c.Aliases {
			if strings.EqualFold(entry, a) {
				return c.ID
			}
		}
	}
	if strings.Contains(entry, "/") {
		return ownerOfPath(reg, entry)
	}
	return ""
}

// conceptsTouched returns the entries of the 'Concepts touched' field
// (fieldValue), split on commas and semicolons and cut before
// explanations ("goal (extend export)").
func conceptsTouched(text string) []string {
	var out []string
	for _, l := range fieldValue(text, "Concepts touched") {
		l = bullet.ReplaceAllString(l, "")
		for _, e := range strings.FieldsFunc(l, func(r rune) bool { return r == ',' || r == ';' }) {
			if e = cleanEntry(e); e != "" && !isNone(e) {
				out = append(out, e)
			}
		}
	}
	return out
}

// fieldValue returns the value of template field name in text: the rest of
// its line, or when that is empty the lines below it up to a blank line,
// heading or another field. Nil when the field is absent or empty.
func fieldValue(text, name string) []string {
	lines := strings.Split(text, "\n")
	re := fieldLine(name)
	for i, l := range lines {
		loc := re.FindStringIndex(l)
		if loc == nil {
			continue
		}
		if rest := strings.TrimSpace(strings.Trim(strings.TrimSpace(l[loc[1]:]), "*_")); rest != "" {
			return []string{rest}
		}
		var raw []string
		for _, next := range lines[i+1:] {
			t := strings.TrimSpace(next)
			if t == "" || strings.HasPrefix(t, "#") || anyField(t) {
				break
			}
			raw = append(raw, t)
		}
		return raw
	}
	return nil
}

// withoutFieldNames removes the template field names from text, so the
// words of "Replaces / removes" do not count as what the bead is about.
func withoutFieldNames(text string) string {
	for _, f := range TemplateFields {
		text = fieldLine(f).ReplaceAllString(text, "")
	}
	return text
}

var bullet = regexp.MustCompile(`^\s*(?:[-*+]|\d+[.)])\s+`)

func cleanEntry(e string) string {
	e = strings.TrimSpace(strings.NewReplacer("`", "", "**", "").Replace(e))
	for _, sep := range []string{" (", " - ", " — ", " – ", ": "} {
		if i := strings.Index(e, sep); i >= 0 {
			e = e[:i]
		}
	}
	return strings.TrimSpace(strings.TrimRight(e, ".:"))
}

func isNone(e string) bool {
	switch strings.ToLower(e) {
	case "none", "n/a", "na", "-", "nothing":
		return true
	}
	return false
}

func anyField(line string) bool {
	for _, f := range TemplateFields {
		if fieldLine(f).MatchString(line) {
			return true
		}
	}
	return false
}

// fieldLine matches a template field as a markdown heading, a bullet or a
// plain line, optionally bold, followed by ':' or the end of the line.
// The match ends after the colon (or the name).
func fieldLine(name string) *regexp.Regexp {
	return regexp.MustCompile(`(?im)^[ \t]*(?:#{1,6}[ \t]*|[-*+][ \t]+)?(?:\*\*|__)?` + phrase(name) +
		`[ \t]*(?:\*\*|__)?[ \t]*(?::|$)`)
}

// mentions reports whether term appears in text as a whole word or
// phrase, case-insensitively.
func mentions(text, term string) bool {
	if strings.TrimSpace(term) == "" {
		return false
	}
	return regexp.MustCompile(`(?i)(?:^|[^\p{L}\p{N}_])` + phrase(term) + `(?:$|[^\p{L}\p{N}_])`).MatchString(text)
}

// phrase quotes term for a regexp, letting its spaces and slashes match
// any surrounding whitespace.
func phrase(term string) string {
	words := strings.Fields(term)
	for i, w := range words {
		words[i] = regexp.QuoteMeta(w)
	}
	return strings.ReplaceAll(strings.Join(words, `\s+`), `/`, `\s*/\s*`)
}

// Run triages the beads named by ids or, when ids is empty, the open beads
// without an arch:* label (created at or after since, unless since is
// zero). A bead that cannot be read or written gets Result.Error and the
// rest still run; err reports only a failed listing.
func Run(c *Client, reg *registry.Registry, ids []string, since, now time.Time, dryRun bool) ([]Result, error) {
	var bs []Bead
	var results []Result
	if len(ids) == 0 {
		all, err := c.List("open", LabelPrefix)
		if err != nil {
			return nil, err
		}
		for _, b := range all {
			if t, ok := b.Created(); since.IsZero() || (ok && !t.Before(since)) {
				bs = append(bs, b)
			}
		}
	}
	for _, id := range ids {
		b, err := c.Show(id)
		if err != nil {
			results = append(results, Result{ID: id, Error: err.Error()})
			continue
		}
		bs = append(bs, b)
	}
	provides := repoProvides(reg, c.Root)
	for _, b := range bs {
		r, err := Triage(c, reg, provides, b, now, dryRun)
		if err != nil {
			r.Error = err.Error()
		}
		results = append(results, r)
	}
	return results, nil
}

// repoProvides reads what each concept of the repository at root exports.
// It is best effort: an unreadable tree only skips the existing-code check.
func repoProvides(reg *registry.Registry, root string) map[string][]symbols.Symbol {
	if root == "" {
		return nil
	}
	files, err := registry.RepoFiles(root)
	if err != nil {
		return nil
	}
	provides, _ := symbols.ByConcept(reg, root, files, nil)
	return provides
}
