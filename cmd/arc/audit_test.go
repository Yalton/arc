package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// auditRegistry adds a removal tracked by bead t-1 to beadsRegistry.
const auditRegistry = beadsRegistry + `removals:
  - id: old-goal
    what: legacy goal
    paths: ["internal/goal/old.go"]
    replaced_by: goal
    remove_by: 2020-01-01
    bead: t-1
`

func auditRepo(t *testing.T) string {
	t.Helper()
	state := beadsRepo(t)
	for rel, content := range map[string]string{
		".arch/registry.yaml":  auditRegistry,
		"internal/goal/old.go": "package goal\n\nvar Old = 1\n",
	} {
		if err := os.WriteFile(filepath.FromSlash(rel), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return state
}

func TestAuditUsage(t *testing.T) {
	auditRepo(t)
	for _, args := range [][]string{{"audit", "--since", "7x"}, {"audit", "extra"}, {"audit", "--bogus"}} {
		if code, _, errOut := runArc(args...); code != exitUsage || errOut == "" {
			t.Errorf("%v: code %d stderr %q", args, code, errOut)
		}
	}
}

func TestAuditReportJSON(t *testing.T) {
	state := auditRepo(t)
	data := `[{"id":"t-1","title":"T","status":"closed"}]`
	if err := os.WriteFile(filepath.Join(state, "show-t-1.json"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := runArc("audit", "--since", "2w", "--json")
	if code != exitOK {
		t.Fatalf("code %d stderr %q out %q", code, errOut, out)
	}
	var rep struct {
		Since    string
		Concepts []struct {
			ID  string
			Now int
		}
		OverdueRemovals    []struct{ ID string } `json:"overdue_removals"`
		ClosedBeadRemovals []struct{ ID string } `json:"closed_bead_removals"`
		Notices            []string
	}
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if rep.Since != "2w" || len(rep.Concepts) != 1 || rep.Concepts[0].Now != 4 {
		t.Errorf("report %+v", rep)
	}
	if len(rep.OverdueRemovals) != 1 || len(rep.ClosedBeadRemovals) != 1 || len(rep.Notices) != 0 {
		t.Errorf("removals %+v closed %+v notices %v", rep.OverdueRemovals, rep.ClosedBeadRemovals, rep.Notices)
	}
}

func TestAuditWithoutBeadsIsNotice(t *testing.T) {
	state := auditRepo(t)
	if err := os.WriteFile(filepath.Join(state, "nows"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, _ := runArc("audit")
	if code != exitOK || !strings.Contains(out, "Notice: beads: no beads workspace") ||
		!strings.Contains(out, "Overdue removals: 1") {
		t.Fatalf("code %d out:\n%s", code, out)
	}
}
