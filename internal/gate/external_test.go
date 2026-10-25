package gate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Yalton/arc/internal/registry"
)

// fakeTool writes an executable script that prints the file "out" in the
// repository root (when present) and exits with the code in "code".
func fakeTool(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "fake-tool")
	script := "#!/bin/sh\n[ -f out ] && cat out\nexit $(cat code 2>/dev/null || echo 0)\n"
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func externalRepo(t *testing.T, command string, baseline bool) *testRepo {
	t.Helper()
	b := "false"
	if baseline {
		b = "true"
	}
	return newRepo(t, "version: 1\nsystem: {name: x, summary: x}\nchecks:\n  - id: lint\n    command: ["+command+"]\n    baseline: "+b+"\n")
}

func TestNormalize(t *testing.T) {
	for in, want := range map[string]string{
		"internal/a/x.go:12:6: unreachable func: Foo": "internal/a/x.go: unreachable func: Foo",
		"x.py:3:  unused   import 'os'":               "x.py: unused import 'os'",
		"  src/b.ts:40 unused export":                 "src/b.ts unused export",
		"took 12:30 to run":                           "took 12:30 to run",
		"a/x.go:82-90: duplicate of b/y.go:5-13":      "a/x.go: duplicate of b/y.go",
	} {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestExternalBaselineFiltersAcceptedAcrossLineShifts(t *testing.T) {
	r := externalRepo(t, fakeTool(t), true)
	r.write("out", "a/x.go:3:1: unused func Old\n")
	k := r.registryCheck()
	res, err := RecordBaseline(r.root, k)
	if err != nil || res.Findings != 1 || res.Added != 1 || res.Removed != 0 {
		t.Fatalf("record: %+v %v", res, err)
	}
	data, _ := os.ReadFile(filepath.Join(r.root, ".arch/baseline/lint.txt"))
	if string(data) != "a/x.go: unused func Old\n" {
		t.Fatalf("baseline file %q", data)
	}

	// The accepted finding moved lines; a new one appeared.
	r.write("out", "a/x.go:9:1: unused func Old\n\n  \na/y.go:2:1: unused func New\n")
	rep := r.gate("staged", "checks")
	if rep.Errors != 1 || !strings.Contains(findings(rep), "[checks.lint] a/y.go:2:1: unused func New") ||
		strings.Contains(findings(rep), "Old") {
		t.Fatalf("got:\n%s", findings(rep))
	}

	// Re-recording reports the change in counts.
	r.write("out", "a/y.go:2:1: unused func New\n")
	if res, err = RecordBaseline(r.root, k); err != nil || res.Added != 1 || res.Removed != 1 || res.Findings != 1 {
		t.Fatalf("re-record: %+v %v", res, err)
	}
	if rep := r.gate("all", "checks.lint"); rep.Errors != 0 {
		t.Fatalf("got:\n%s", findings(rep))
	}
}

func TestExternalBaselineIsMultiset(t *testing.T) {
	r := externalRepo(t, fakeTool(t), true)
	r.write("out", "a/x.go:3:1: unused func Old\nb.go:1: dup\nb.go:5: dup\n")
	k := r.registryCheck()
	res, err := RecordBaseline(r.root, k)
	if err != nil || res.Findings != 3 || res.Added != 3 ||
		strings.Join(res.AddedSample, "|") != "a/x.go: unused func Old|b.go: dup|b.go: dup" {
		t.Fatalf("record: %+v %v", res, err)
	}
	data, _ := os.ReadFile(filepath.Join(r.root, ".arch/baseline/lint.txt"))
	if string(data) != "a/x.go: unused func Old\nb.go: dup\nb.go: dup\n" {
		t.Fatalf("baseline file %q", data)
	}
	if rep := r.gate("all", "checks.lint"); rep.Errors != 0 {
		t.Fatalf("got:\n%s", findings(rep))
	}
	// A new finding identical (after normalization) to accepted ones fails:
	// only the excess copies are reported.
	r.write("out", "a/x.go:3:1: unused func Old\na/x.go:8:1: unused func Old\nb.go:1: dup\nb.go:5: dup\nb.go:9: dup\n")
	rep := r.gate("all", "checks.lint")
	if rep.Errors != 2 || !strings.Contains(findings(rep), "a/x.go:8:1: unused func Old") ||
		!strings.Contains(findings(rep), "b.go:9: dup") {
		t.Fatalf("got:\n%s", findings(rep))
	}
	// Re-recording counts copies.
	if res, err = RecordBaseline(r.root, k); err != nil || res.Findings != 5 || res.Added != 2 || res.Removed != 0 {
		t.Fatalf("re-record: %+v %v", res, err)
	}
}

func TestExternalWithoutBaselineFailsEveryFinding(t *testing.T) {
	r := externalRepo(t, fakeTool(t), false)
	r.write(".arch/baseline/lint.txt", "a/x.go: old\n")
	r.write("out", "a/x.go:1: old\n")
	r.write("code", "1\n")
	if rep := r.gate("all", "checks"); rep.Errors != 1 || !strings.Contains(findings(rep), "a/x.go:1: old") {
		t.Fatalf("got:\n%s", findings(rep))
	}
}

func TestExternalCommandProblems(t *testing.T) {
	r := externalRepo(t, `"no-such-tool-arc-test", "./..."`, true)
	rep := r.gate("all", "checks")
	if rep.Errors != 1 || !strings.Contains(findings(rep), `command "no-such-tool-arc-test" not found. Install it`) {
		t.Fatalf("got:\n%s", findings(rep))
	}

	r = externalRepo(t, fakeTool(t), true)
	r.write("code", "3\n")
	rep = r.gate("all", "checks.lint")
	if rep.Errors != 1 || !strings.Contains(findings(rep), "check lint failed: exit status 3") {
		t.Fatalf("got:\n%s", findings(rep))
	}
}

func TestExternalChecksSelection(t *testing.T) {
	r := externalRepo(t, fakeTool(t), false)
	r.render()
	if rep := r.gate("all"); rep.Checks[len(rep.Checks)-1] != "checks.lint" {
		t.Fatalf("checks %v", rep.Checks)
	}
	if rep := r.gate("all", "checks"); len(rep.Checks) != 1 || rep.Checks[0] != "checks.lint" {
		t.Fatalf("checks %v", rep.Checks)
	}
	reg, _ := registry.Load(r.root)
	_, err := Run(&Context{Root: r.root, Reg: reg}, []string{"checks.nope"})
	if err == nil || !strings.Contains(err.Error(), `unknown check "checks.nope"`) || !strings.Contains(err.Error(), "checks.lint") {
		t.Fatalf("err %v", err)
	}
}

func TestBaselinePathRejectsUnsafeIDs(t *testing.T) {
	for _, id := range []string{"../x", "a/b", "", ".hidden", "a..b"} {
		if _, err := BaselinePath(id); err == nil {
			t.Errorf("%q accepted", id)
		}
	}
}

func (r *testRepo) registryCheck() registry.Check {
	r.t.Helper()
	reg, err := registry.Load(r.root)
	if err != nil {
		r.t.Fatal(err)
	}
	return reg.Checks[0]
}

func TestExternalTimeout(t *testing.T) {
	p := filepath.Join(t.TempDir(), "slow")
	if err := os.WriteFile(p, []byte("#!/bin/sh\nexec sleep 5\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	old := ExternalTimeout
	ExternalTimeout = 100 * time.Millisecond
	defer func() { ExternalTimeout = old }()
	r := externalRepo(t, p, false)
	if rep := r.gate("all", "checks"); rep.Errors != 1 || !strings.Contains(findings(rep), "timed out after 100ms") {
		t.Fatalf("got:\n%s", findings(rep))
	}
}
