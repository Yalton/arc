package audit

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Yalton/arc/internal/registry"
)

var update = flag.Bool("update", false, "rewrite golden files in testdata/audit")

const goldenDir = "../../testdata/audit"

func golden(t *testing.T, name, got string) {
	t.Helper()
	p := filepath.Join(goldenDir, name)
	if *update {
		if err := os.WriteFile(p, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("%v (run go test ./internal/audit/ -update)", err)
	}
	if got != string(want) {
		t.Errorf("%s mismatch\n--- got ---\n%s--- want ---\n%s", name, got, want)
	}
}

// sampleReport is a fixed report, independent of git, for golden output.
func sampleReport() *Report {
	ratio := 0.5
	return &Report{
		Since: "7d", From: "2026-01-08T12:00:00Z", To: "2026-01-15T12:00:00Z",
		Base:               "0123456789abcdef0123456789abcdef01234567",
		Concepts:           []ConceptLOC{{"alpha", 120, 180, 60}, {"beta", 0, 40, 40}},
		History:            History{Commits: 6, Added: 300, Deleted: 120, Fix: 2, Feat: 4, FixFeatRatio: &ratio},
		Hotspots:           []Hotspot{{"internal/a/a.go", 2}},
		Unowned:            []string{"other/x.go"},
		Drafts:             []string{"beta"},
		NoReason:           []string{"alpha"},
		MultiStorage:       []ConceptStorage{{"beta", []string{"t1", "t2"}}},
		ExpiredFlags:       []FlagItem{{Name: "OLD_FLAG", Concept: "alpha", Expires: "2026-01-10", Bead: "t-9"}},
		OverdueRemovals:    []RemovalItem{{ID: "old-a", What: "legacy a", ReplacedBy: "alpha", RemoveBy: "2026-01-14", Bead: "t-1"}},
		ClosedBeadRemovals: []RemovalItem{},
		Checks:             []CheckResult{{ID: "checks.lint", Findings: 2, Baselined: 1, New: []string{"y.go:2: worse"}}},
		Notices:            []string{},
	}
}

func TestPromptGolden(t *testing.T) {
	reg, err := registry.Parse([]byte(testRegistry))
	if err != nil {
		t.Fatal(err)
	}
	got := Prompt(sampleReport(), reg, testNow)
	for _, want := range []string{"arch:consolidation", "arc:<entry-id>", "### Proposed registry diff", "### Beads to file",
		"checks.lint: 2 finding(s), 1 baselined, 1 new", "System: demo", "never runs a model"} {
		if !strings.Contains(got, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
	if got != Prompt(sampleReport(), reg, testNow) {
		t.Error("prompt is not deterministic")
	}
	golden(t, "prompt.txt", got+"\n")
}

func TestReportTextGolden(t *testing.T) {
	golden(t, "report.txt", sampleReport().Text()+"\n")
}
