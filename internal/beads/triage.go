package beads

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"arc/internal/brief"
	"arc/internal/registry"
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
// leave them out of the analysed text and skip identical notes.
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

// Analyze triages b against reg without touching beads. The returned
// Note is what Triage would append.
func Analyze(reg *registry.Registry, b Bead, now time.Time) Result {
	text := Text(b)
	r := Result{ID: b.ID, Title: b.Title}
	for _, f := range TemplateFields {
		if fieldLine(f).FindStringIndex(text) == nil {
			r.MissingFields = append(r.MissingFields, f)
			r.Reasons = append(r.Reasons, "missing template field: "+f)
		}
	}
	resolved := map[string]bool{}
	for _, id := range ResolveConcepts(reg, text) {
		resolved[id] = true
	}
	for _, e := range conceptsTouched(text) {
		if id := conceptFor(reg, e); id != "" {
			resolved[id] = true
		} else {
			r.UnknownConcepts = append(r.UnknownConcepts, e)
			r.Reasons = append(r.Reasons, fmt.Sprintf(
				"'Concepts touched' names %q, which is not in the registry; use an existing concept or add it to %s with a reason",
				e, registry.Path))
		}
	}
	for _, c := range reg.Concepts {
		if resolved[c.ID] {
			r.Concepts = append(r.Concepts, c.ID)
		}
	}
	if len(r.Concepts) == 0 {
		r.Reasons = append(r.Reasons, "no registry concept resolved from the bead text; name concept ids, aliases or repo paths")
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

func note(reg *registry.Registry, r Result, now time.Time) string {
	var w strings.Builder
	w.WriteString(noteBegin + "\n" + r.Label + "\n")
	for _, reason := range r.Reasons {
		w.WriteString("- " + reason + "\n")
	}
	if len(r.Concepts) > 0 {
		if b, err := brief.ForConcepts(reg, r.Concepts, now); err == nil {
			w.WriteString("arc brief:\n" + b.Text())
		}
	}
	w.WriteString(noteEnd)
	return w.String()
}

// Triage analyses b and, unless dryRun, appends the note (skipped when an
// identical one is already there) and adds the label (skipped when
// present). It never removes labels.
func Triage(c *Client, reg *registry.Registry, b Bead, now time.Time, dryRun bool) (Result, error) {
	r := Analyze(reg, b, now)
	if dryRun {
		return r, nil
	}
	if !strings.Contains(b.Notes, r.Note) {
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
	return r, nil
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

// conceptsTouched returns the entries of the 'Concepts touched' field:
// the rest of its line, or when that is empty the lines below it up to a
// blank line, heading or another field. Entries are split on commas and
// semicolons and cut before explanations ("goal (extend export)").
func conceptsTouched(text string) []string {
	lines := strings.Split(text, "\n")
	re := fieldLine("Concepts touched")
	var raw []string
	for i, l := range lines {
		loc := re.FindStringIndex(l)
		if loc == nil {
			continue
		}
		rest := strings.TrimSpace(strings.Trim(strings.TrimSpace(l[loc[1]:]), "*_"))
		if rest != "" {
			raw = append(raw, rest)
			break
		}
		for _, next := range lines[i+1:] {
			t := strings.TrimSpace(next)
			if t == "" || strings.HasPrefix(t, "#") || anyField(t) {
				break
			}
			raw = append(raw, t)
		}
		break
	}
	var out []string
	for _, l := range raw {
		l = bullet.ReplaceAllString(l, "")
		for _, e := range strings.FieldsFunc(l, func(r rune) bool { return r == ',' || r == ';' }) {
			if e = cleanEntry(e); e != "" && !isNone(e) {
				out = append(out, e)
			}
		}
	}
	return out
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
	for _, b := range bs {
		r, err := Triage(c, reg, b, now, dryRun)
		if err != nil {
			r.Error = err.Error()
		}
		results = append(results, r)
	}
	return results, nil
}
