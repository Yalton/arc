package beads

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"arc/internal/registry"
)

var now = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

const triageRegistry = `version: 1
system: {name: demo, summary: Demo system.}
non_goals:
  - id: no-web-ui
    text: No browser UI.
    keywords: [web ui, dashboard]
concepts:
  - id: goal
    name: Goal
    summary: Operator goals.
    paths: ["internal/goal/**"]
    aliases: [objective]
    rules: [Goals are work nodes.]
  - id: sched
    name: Scheduler
    summary: Runs jobs.
    paths: ["internal/sched/**"]
`

func testRegistry(t *testing.T) *registry.Registry {
	t.Helper()
	reg, err := registry.Parse([]byte(triageRegistry))
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

const goodDesc = `Export goals as CSV.

## Replaces / removes
none

**Concepts touched:** goal

Closest existing thing: internal/goal/export.go
`

func TestAnalyzeOK(t *testing.T) {
	r := Analyze(testRegistry(t), Bead{ID: "t-1", Title: "Goal export", Description: goodDesc}, now)
	if r.Label != LabelOK || len(r.Reasons) != 0 || !reflect.DeepEqual(r.Concepts, []string{"goal"}) {
		t.Fatalf("got %+v", r)
	}
	if !strings.HasPrefix(r.Note, noteBegin+"\narch:ok\narc brief:\nSystem: demo") ||
		!strings.Contains(r.Note, "Goals are work nodes.") || !strings.HasSuffix(r.Note, noteEnd) {
		t.Fatalf("note %q", r.Note)
	}
}

func TestAnalyzeMissingFields(t *testing.T) {
	r := Analyze(testRegistry(t), Bead{ID: "t-1", Description: "Improve the objective handling.\nConcepts touched: goal"}, now)
	if r.Label != LabelReview || !reflect.DeepEqual(r.MissingFields, []string{"Replaces / removes", "Closest existing thing"}) {
		t.Fatalf("got %+v", r)
	}
	if !strings.Contains(r.Note, "- missing template field: Closest existing thing") {
		t.Fatalf("note %q", r.Note)
	}
}

func TestAnalyzeNonGoal(t *testing.T) {
	desc := goodDesc + "\nAlso add a Web  UI to browse goals.\n"
	r := Analyze(testRegistry(t), Bead{ID: "t-1", Description: desc}, now)
	if r.Label != LabelReview || len(r.NonGoals) != 1 || r.NonGoals[0] != (NonGoalHit{"no-web-ui", "web ui"}) {
		t.Fatalf("got %+v", r)
	}
	// Whole words only: "dashboards" is not the keyword "dashboard".
	r = Analyze(testRegistry(t), Bead{ID: "t-1", Description: goodDesc + "dashboards"}, now)
	if len(r.NonGoals) != 0 {
		t.Fatalf("got %+v", r.NonGoals)
	}
}

func TestAnalyzeUnknownConcept(t *testing.T) {
	desc := strings.Replace(goodDesc, "**Concepts touched:** goal", "Concepts touched:\n- goal (export)\n- exporter", 1)
	r := Analyze(testRegistry(t), Bead{ID: "t-1", Description: desc}, now)
	if r.Label != LabelReview || !reflect.DeepEqual(r.UnknownConcepts, []string{"exporter"}) ||
		!reflect.DeepEqual(r.Concepts, []string{"goal"}) {
		t.Fatalf("got %+v", r)
	}
}

func TestAnalyzeNoConcept(t *testing.T) {
	desc := "Replaces / removes: none\nConcepts touched: none\nClosest existing thing: nothing"
	r := Analyze(testRegistry(t), Bead{ID: "t-1", Description: desc}, now)
	if r.Label != LabelReview || len(r.Concepts) != 0 || !strings.Contains(r.Summary(), "no registry concept resolved") {
		t.Fatalf("got %+v", r)
	}
	if strings.Contains(r.Note, "arc brief:") {
		t.Fatalf("no brief without concepts: %q", r.Note)
	}
}

func TestResolveConcepts(t *testing.T) {
	reg := testRegistry(t)
	for text, want := range map[string][]string{
		"touches internal/sched/run.go":         {"sched"},
		"see internal/sched/ and the OBJECTIVE": {"goal", "sched"},
		"internal/sched/** globs":               {"sched"},
		"goals and schedules":                   nil,
	} {
		if got := ResolveConcepts(reg, text); !reflect.DeepEqual(got, want) {
			t.Errorf("%q: got %v want %v", text, got, want)
		}
	}
}

func TestTextDropsArcNotes(t *testing.T) {
	r := Analyze(testRegistry(t), Bead{ID: "t-1", Description: goodDesc}, now)
	// A previous arc note lists non-goal text; it must not count as a hit.
	b := Bead{ID: "t-1", Description: goodDesc, Notes: "human note\n" + strings.Replace(r.Note, "arc brief:", "dashboard web ui", 1)}
	if r2 := Analyze(testRegistry(t), b, now); r2.Label != LabelOK || !strings.Contains(Text(b), "human note") {
		t.Fatalf("got %+v", r2)
	}
}

func TestTriageWritesOnce(t *testing.T) {
	c, state := fakeBd(t)
	reg := testRegistry(t)
	b := Bead{ID: "t-1", Description: goodDesc}
	r, err := Triage(c, reg, b, now, false)
	if err != nil || !r.NoteAdded || !r.LabelAdded {
		t.Fatalf("got %+v %v", r, err)
	}
	if calls := readState(t, state, "calls"); calls != "note t-1 --stdin\nlabel add t-1 arch:ok\n" {
		t.Fatalf("calls %q", calls)
	}
	// Already triaged with the same outcome: nothing to write.
	b.Notes, b.Labels = "earlier\n"+r.Note, []string{LabelOK}
	if r, err = Triage(c, reg, b, now, false); err != nil || r.NoteAdded || r.LabelAdded {
		t.Fatalf("got %+v %v", r, err)
	}
	if calls := readState(t, state, "calls"); strings.Count(calls, "\n") != 2 {
		t.Fatalf("calls %q", calls)
	}
}

func TestTriageSkipsNoteWithSameDecision(t *testing.T) {
	c, state := fakeBd(t)
	reg := testRegistry(t)
	b := Bead{ID: "t-1", Description: goodDesc, Labels: []string{LabelOK}}
	r := Analyze(reg, b, now)
	// The brief in an earlier note differs (written on another day): the
	// decision is the same, so no duplicate note.
	old := strings.Replace(r.Note, noteBrief, noteBrief+"brief from yesterday\n", 1)
	b.Notes = old
	if r, err := Triage(c, reg, b, now.Add(48*time.Hour), false); err != nil || r.NoteAdded {
		t.Fatalf("got %+v %v", r, err)
	}
	if calls := readState(t, state, "calls"); calls != "" {
		t.Fatalf("calls %q", calls)
	}
	// Only the most recent note counts: a later arch:review note means the
	// decision changed back, so a new note is written.
	review := Analyze(reg, Bead{ID: "t-1", Description: "no template"}, now).Note
	b.Notes = old + "\n" + review
	if r, err := Triage(c, reg, b, now, false); err != nil || !r.NoteAdded {
		t.Fatalf("got %+v %v", r, err)
	}
}

func TestTriageSwapsOppositeLabel(t *testing.T) {
	c, state := fakeBd(t)
	reg := testRegistry(t)
	b := Bead{ID: "t-1", Description: "no template", Labels: []string{LabelOK, "keep"}}
	r, err := Triage(c, reg, b, now, false)
	if err != nil || !r.LabelAdded || r.LabelRemoved != LabelOK {
		t.Fatalf("got %+v %v", r, err)
	}
	calls := readState(t, state, "calls")
	if !strings.Contains(calls, "label add t-1 arch:review\nlabel remove t-1 arch:ok\n") || strings.Contains(calls, "keep") {
		t.Fatalf("calls %q", calls)
	}
	// Fixed and re-triaged: arch:ok replaces arch:review.
	b = Bead{ID: "t-2", Description: goodDesc, Labels: []string{LabelReview, "keep"}}
	if r, err = Triage(c, reg, b, now, false); err != nil || !r.LabelAdded || r.LabelRemoved != LabelReview {
		t.Fatalf("got %+v %v", r, err)
	}
	calls = readState(t, state, "calls")
	if !strings.Contains(calls, "label add t-2 arch:ok\nlabel remove t-2 arch:review\n") || strings.Contains(calls, "keep") {
		t.Fatalf("calls %q", calls)
	}
	// Only the opposite label is removed, and only when present.
	b = Bead{ID: "t-3", Description: goodDesc, Labels: []string{"arch:consolidation"}}
	if r, err = Triage(c, reg, b, now, false); err != nil || r.LabelRemoved != "" {
		t.Fatalf("got %+v %v", r, err)
	}
	if calls = readState(t, state, "calls"); strings.Contains(calls, "remove t-3") {
		t.Fatalf("calls %q", calls)
	}
}

func TestTriageDryRun(t *testing.T) {
	c, state := fakeBd(t)
	r, err := Triage(c, testRegistry(t), Bead{ID: "t-1", Description: goodDesc}, now, true)
	if err != nil || r.Label != LabelOK || r.NoteAdded || r.LabelAdded || r.Note == "" {
		t.Fatalf("got %+v %v", r, err)
	}
	if calls := readState(t, state, "calls"); calls != "" {
		t.Fatalf("dry run ran bd: %q", calls)
	}
}
