package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const beadsRegistry = `version: 1
system: {name: demo, summary: Demo.}
non_goals:
  - id: no-web-ui
    text: No browser UI.
    keywords: [dashboard]
concepts:
  - id: goal
    name: Goal
    summary: Operator goals.
    paths: ["internal/goal/**"]
    rules: [Goals are work nodes.]
`

// beadsRepo makes a git repo with a registry, chdirs into it and puts the
// fake bd (testdata/fakebd) first on PATH. It returns the fake's state dir.
func beadsRepo(t *testing.T) string {
	t.Helper()
	fake, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fakebd"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	cmd := exec.Command("git", "init", "-q", root)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	for rel, content := range map[string]string{
		".arch/registry.yaml":   beadsRegistry,
		"internal/goal/goal.go": "package goal\n",
	} {
		p := filepath.Join(root, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(root)
	state := t.TempDir()
	t.Setenv("FAKEBD_DIR", state)
	t.Setenv("PATH", fake+string(os.PathListSeparator)+os.Getenv("PATH"))
	return state
}

func fakeShow(t *testing.T, state, id, desc string, labels ...string) {
	t.Helper()
	b := map[string]any{"id": id, "title": "T " + id, "description": desc, "status": "open", "labels": labels}
	data, _ := json.Marshal([]any{b})
	if err := os.WriteFile(filepath.Join(state, "show-"+id+".json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func fakeState(t *testing.T, state, name string) string {
	t.Helper()
	data, _ := os.ReadFile(filepath.Join(state, name))
	return string(data)
}

const okDesc = "Replaces / removes: none\nConcepts touched: goal\nClosest existing thing: internal/goal"

func TestTriageOKAndReview(t *testing.T) {
	state := beadsRepo(t)
	fakeShow(t, state, "t-1", okDesc)
	fakeShow(t, state, "t-2", okDesc+"\nAdd a dashboard.")
	fakeShow(t, state, "t-3", "Concepts touched: exporter")
	code, out, errOut := runArc("triage", "t-1", "t-2", "t-3")
	if code != exitOK {
		t.Fatalf("code %d stderr %q", code, errOut)
	}
	for _, want := range []string{
		"t-1: arch:ok (concepts: goal)",
		`t-2: arch:review: matches non-goal no-web-ui (keyword "dashboard")`,
		"t-3: arch:review: missing template field: Replaces / removes",
		`names "exporter", which is not in the registry`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	calls := fakeState(t, state, "calls")
	for _, want := range []string{"label add t-1 arch:ok", "label add t-2 arch:review", "label add t-3 arch:review", "note t-1 --stdin"} {
		if !strings.Contains(calls, want) {
			t.Errorf("missing call %q in %q", want, calls)
		}
	}
	if !strings.Contains(fakeState(t, state, "stdin"), "Goals are work nodes.") {
		t.Error("brief not written as a note")
	}
}

func TestTriageDryRunJSON(t *testing.T) {
	state := beadsRepo(t)
	fakeShow(t, state, "t-1", okDesc)
	code, out, _ := runArc("triage", "t-1", "--dry-run", "--json")
	var rep triageReport
	if code != exitOK || json.Unmarshal([]byte(out), &rep) != nil || !rep.DryRun ||
		len(rep.Results) != 1 || rep.Results[0].Label != "arch:ok" || rep.Results[0].Note == "" {
		t.Fatalf("code %d out %s", code, out)
	}
	if calls := fakeState(t, state, "calls"); strings.Contains(calls, "label") || strings.Contains(calls, "note") {
		t.Fatalf("dry run wrote: %q", calls)
	}
}

func TestTriageNewSelectsUnlabelled(t *testing.T) {
	state := beadsRepo(t)
	list := `[{"id": "t-1", "status": "open", "description": "x", "labels": ["arch:ok"]},
	          {"id": "t-2", "status": "open", "description": "Concepts touched: goal", "labels": ["other"]}]`
	os.WriteFile(filepath.Join(state, "list.json"), []byte(list), 0o644)
	code, out, _ := runArc("triage", "--new")
	if code != exitOK || strings.Contains(out, "t-1") || !strings.Contains(out, "t-2: arch:review") {
		t.Fatalf("code %d out %s", code, out)
	}
}

func TestTriageWithoutBeadsIsNotice(t *testing.T) {
	state := beadsRepo(t)
	os.WriteFile(filepath.Join(state, "nows"), nil, 0o644)
	code, out, _ := runArc("triage", "--new")
	if code != exitOK || !strings.Contains(out, "bead features skipped") {
		t.Fatalf("code %d out %q", code, out)
	}
}

func TestTriageUsageAndMissingBead(t *testing.T) {
	beadsRepo(t)
	for _, args := range [][]string{{"triage"}, {"triage", "--new", "t-1"}, {"triage", "--bogus"}} {
		if code, _, _ := runArc(args...); code != exitUsage {
			t.Errorf("%v: code %d", args, code)
		}
	}
	code, out, _ := runArc("triage", "t-404")
	if code != exitUsage || !strings.Contains(out, "t-404: error:") {
		t.Fatalf("code %d out %q", code, out)
	}
}

func TestBriefBead(t *testing.T) {
	state := beadsRepo(t)
	fakeShow(t, state, "t-1", "Touches internal/goal/x.go")
	code, out, _ := runArc("brief", "--bead", "t-1")
	if code != exitOK || !strings.Contains(out, "Concept goal (Goal)") || !strings.Contains(out, "Goals are work nodes.") {
		t.Fatalf("code %d out %q", code, out)
	}
	fakeShow(t, state, "t-2", "nothing relevant")
	if code, out, errOut := runArc("brief", "--bead", "t-2"); code != exitOK || strings.Contains(out, "Concept goal") ||
		!strings.Contains(errOut, "no concepts resolved") {
		t.Fatalf("code %d out %q stderr %q", code, out, errOut)
	}
	if code, _, errOut := runArc("brief", "--bead", "t-404"); code != exitUsage || !strings.Contains(errOut, "not found") {
		t.Fatalf("code %d stderr %q", code, errOut)
	}
	if code, _, _ := runArc("brief", "--bead", "t-1", "--concept", "goal"); code != exitUsage {
		t.Fatalf("code %d", code)
	}
	os.WriteFile(filepath.Join(state, "nows"), nil, 0o644)
	if code, _, errOut := runArc("brief", "--bead", "t-1"); code != exitUsage || !strings.Contains(errOut, "bead features skipped") {
		t.Fatalf("code %d stderr %q", code, errOut)
	}
}
