package gate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// cloneRepo registers fakeTool under check id (dupl or jscpd) with a baseline.
func cloneRepo(t *testing.T, id string) *testRepo {
	t.Helper()
	return newRepo(t, "version: 1\nsystem: {name: x, summary: x}\nchecks:\n  - id: "+id+
		"\n    command: ["+fakeTool(t)+"]\n    baseline: true\n")
}

func TestCloneLinesJscpd(t *testing.T) {
	got := cloneLines(JscpdID, []string{
		"src/b.ts:4:2: warning: Found 9 lines (4-12) duplicated on file src/a.ts (20-28)",
		"a.js:1:14: warning: Found 8 lines (1-9) duplicated on file b.js (1-9)",
		"Found 2 clones.",
		"time: 3.305ms",
		"error: something unexpected",
	})
	want := []string{
		"a.js:1-9: duplicate of b.js:1-9",
		"error: something unexpected",
		"src/b.ts:4-12: duplicate of src/a.ts:20-28",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got:\n%s", strings.Join(got, "\n"))
	}
}

func TestCloneDetectorGateMessageAndBaseline(t *testing.T) {
	for _, c := range []struct{ id, before, after string }{
		{DuplID,
			"a/x.go:10-20: duplicate of b/y.go:3-13\n",
			"a/x.go:40-50: duplicate of b/y.go:3-13\nc/z.go:1-9: duplicate of b/y.go:30-38\n"},
		{JscpdID,
			"a/x.js:10:1: warning: Found 10 lines (10-20) duplicated on file b/y.js (3-13)\nFound 1 clones.\ntime: 1.2ms\n",
			"c/z.js:1:4: warning: Found 8 lines (1-9) duplicated on file b/y.js (30-38)\n" +
				"a/x.js:40:1: warning: Found 10 lines (40-50) duplicated on file b/y.js (3-13)\nFound 2 clones.\ntime: 9.9ms\n"},
	} {
		t.Run(c.id, func(t *testing.T) {
			r := cloneRepo(t, c.id)
			r.write("out", c.before)
			if _, err := RecordBaseline(r.root, r.registryCheck()); err != nil {
				t.Fatal(err)
			}
			data, _ := os.ReadFile(filepath.Join(r.root, ".arch", "baseline", c.id+".txt"))
			if ext := map[string]string{DuplID: "go", JscpdID: "js"}[c.id]; string(data) != "a/x."+ext+": duplicate of b/y."+ext+"\n" {
				t.Fatalf("baseline %q", data)
			}
			// The accepted clone shifted lines (and jscpd's timing changed); a new one appeared.
			r.write("out", c.after)
			rep := r.gate("all", "checks."+c.id)
			got := findings(rep)
			if rep.Errors != 1 || !strings.Contains(got, "[checks."+c.id+"] duplicates existing code: c/z.") ||
				!strings.Contains(got, ":1-9: duplicate of b/y.") ||
				!strings.Contains(got, "; extend or reuse the existing code instead of copying (not in .arch/baseline/"+c.id+".txt; fix it)") {
				t.Fatalf("got:\n%s", got)
			}
		})
	}
}
