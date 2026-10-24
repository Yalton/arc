package brief

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"arc/internal/registry"
)

var update = flag.Bool("update", false, "rewrite golden files in testdata/brief")

var now = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

const fixtureDir = "../../testdata/brief"

func loadFixture(t *testing.T) *registry.Registry {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(fixtureDir, "registry.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	reg, err := registry.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	p := filepath.Join(fixtureDir, name)
	if *update {
		if err := os.WriteFile(p, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("%v (run go test ./internal/brief/ -update)", err)
	}
	if got != string(want) {
		t.Errorf("%s mismatch\n--- got ---\n%s--- want ---\n%s", name, got, want)
	}
}

func TestPlace(t *testing.T) {
	reg := loadFixture(t)
	cases := []struct {
		path string
		want PlaceResult
	}{
		{"internal/workgraph/goal.go", PlaceResult{Path: "internal/workgraph/goal.go", Status: StatusOwned, Concept: "goal"}},
		{"internal/workgraph/goal_test.go", PlaceResult{Path: "internal/workgraph/goal_test.go", Status: StatusIgnored}},
		{"docs/readme.md", PlaceResult{Path: "docs/readme.md", Status: StatusIgnored}},
		// All internal/* concepts share one segment; goal and scheduler
		// also match one alias each (objective, sched) and keep registry
		// order between them; shared beats api and queue on registry order.
		{"internal/objective/sched.go", PlaceResult{Path: "internal/objective/sched.go", Status: StatusUnowned, Closest: []Suggestion{
			{ID: "goal", Prefix: "internal/workgraph"},
			{ID: "scheduler", Prefix: "internal/scheduler"},
			{ID: "shared", Prefix: "internal/util"},
		}}},
		// An alias match breaks the tie among one-segment concepts.
		{"internal/api2/helpers.go", PlaceResult{Path: "internal/api2/helpers.go", Status: StatusUnowned, Closest: []Suggestion{
			{ID: "shared", Prefix: "internal/util"},
			{ID: "goal", Prefix: "internal/workgraph"},
			{ID: "scheduler", Prefix: "internal/scheduler"},
		}}},
		// Two shared segments beat one segment plus an alias.
		{"internal/queue/v2/goal.go", PlaceResult{Path: "internal/queue/v2/goal.go", Status: StatusUnowned, Closest: []Suggestion{
			{ID: "queue", Prefix: "internal/queue"},
			{ID: "goal", Prefix: "internal/workgraph"},
			{ID: "shared", Prefix: "internal/util"},
		}}},
		// Concepts with no shared segment and no alias are left out.
		{"cmd/sched2/main.go", PlaceResult{Path: "cmd/sched2/main.go", Status: StatusUnowned, Closest: []Suggestion{
			{ID: "scheduler", Prefix: "cmd/sched"},
		}}},
		{"zzz/x.go", PlaceResult{Path: "zzz/x.go", Status: StatusUnowned}},
	}
	var msgs []string
	for _, c := range cases {
		got := Place(reg, c.path)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("Place(%q) = %+v, want %+v", c.path, got, c.want)
		}
		msgs = append(msgs, PlaceMessage(got))
	}
	golden(t, "place.txt", strings.Join(msgs, "\n")+"\n")
}

func TestPlaceMessageSpecExample(t *testing.T) {
	r := PlaceResult{Path: "internal/foo/x.go", Status: StatusUnowned, Closest: []Suggestion{{ID: "goal", Prefix: "internal/workgraph"}, {ID: "shared"}}}
	want := "internal/foo/x.go: unowned. Closest: goal (internal/workgraph), shared. Extend one, or add a concept to .arch/registry.yaml with a reason."
	if got := PlaceMessage(r); got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestLiteralPrefix(t *testing.T) {
	for in, want := range map[string]string{
		"internal/workgraph/**": "internal/workgraph",
		"cmd/arc/main.go":       "cmd/arc",
		"**/*.go":               "",
		"main.go":               "",
		"internal/a/{b,c}/**":   "internal/a",
	} {
		if got := LiteralPrefix(in); got != want {
			t.Errorf("LiteralPrefix(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBriefForPaths(t *testing.T) {
	reg := loadFixture(t)
	b := ForPaths(reg, []string{"internal/api/v1/users.go", "internal/scheduler/run.go", "internal/api/server.go", "docs/x.md", "zzz/x.go"}, now)
	golden(t, "paths.txt", b.Text())
	js, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "paths.json", string(js)+"\n")
}

func TestBriefForConcept(t *testing.T) {
	reg := loadFixture(t)
	b, err := ForConcepts(reg, []string{"scheduler"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if n := b.Tokens(); n > 1500 {
		t.Errorf("concept brief is %d tokens, want <= 1500", n)
	}
	golden(t, "concept.txt", b.Text())
}

func TestBriefWhole(t *testing.T) {
	reg := loadFixture(t)
	b := Whole(reg, now)
	if n := b.Tokens(); n > WholeTokenBudget {
		t.Errorf("whole brief is %d tokens, want <= %d", n, WholeTokenBudget)
	}
	golden(t, "whole.txt", b.Text())
}

func TestBriefUnknownConcept(t *testing.T) {
	reg := loadFixture(t)
	if _, err := ForConcepts(reg, []string{"goal", "nope"}, now); err == nil || !strings.Contains(err.Error(), `"nope"`) {
		t.Errorf("err = %v, want unknown concept \"nope\"", err)
	}
}
