package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestFind(t *testing.T) {
	beadsRepo(t)
	src := "package goal\n\n// ExportGoals writes goals as CSV.\nfunc ExportGoals() {}\n"
	if err := os.WriteFile("internal/goal/goal.go", []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, _ := runArc("find", "export", "goals", "to", "csv")
	if code != exitOK || !strings.Contains(out, "- goal (Goal): Operator goals. [goals (id)]") ||
		!strings.Contains(out, "- internal/goal/goal.go:4 ExportGoals - ExportGoals writes goals as CSV. (goal)") {
		t.Fatalf("code %d, out:\n%s", code, out)
	}
	code, out, _ = runArc("--json", "find", "csv")
	var res struct {
		Symbols []struct{ Name, Concept string }
	}
	if code != exitOK || json.Unmarshal([]byte(out), &res) != nil || len(res.Symbols) != 1 || res.Symbols[0].Concept != "goal" {
		t.Fatalf("json: code %d, out %s", code, out)
	}
	if code, out, _ := runArc("find", "kubernetes"); code != exitViolations || !strings.Contains(out, "nothing matches") {
		t.Fatalf("no match: code %d, out %q", code, out)
	}
	if code, _, _ := runArc("find"); code != exitUsage {
		t.Fatalf("no words: code %d", code)
	}
}
