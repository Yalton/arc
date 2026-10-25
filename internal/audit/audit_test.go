package audit

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Yalton/arc/internal/registry"
)

const testRegistry = `version: 1
system: {name: demo, summary: Demo.}
ignore: ["**/*_test.go", ".arch/**"]
concepts:
  - id: alpha
    name: Alpha
    summary: A.
    paths: ["internal/a/**"]
  - id: beta
    name: Beta
    summary: B.
    paths: ["internal/b/**"]
    storage: [t1, t2]
    draft: true
flags:
  - {name: OLD_FLAG, concept: alpha, purpose: Rollout., expires: 2026-01-10, bead: t-9}
  - {name: LIVE_FLAG, concept: alpha, expires: 2026-01-15}
removals:
  - {id: old-a, what: legacy a, paths: ["internal/a/legacy.go"], replaced_by: alpha, remove_by: 2026-01-14, bead: t-1}
  - {id: later, paths: ["internal/b/**"], remove_by: 2027-01-01, bead: t-2}
  - {id: done, paths: ["internal/gone/**"], remove_by: 2026-01-01}
checks:
  - id: lint
    command: ["sh", "-c", "echo 'x.go:1: bad'; echo 'y.go:2: worse'"]
    baseline: true
  - id: missing
    command: ["arc-test-no-such-command"]
`

// testNow is the clock of the fixture: the 7d window starts 2026-01-08.
var testNow = time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)

func git(t *testing.T, dir, date string, args ...string) string {
	t.Helper()
	full := append([]string{"-C", dir, "-c", "init.defaultBranch=main", "-c", "user.name=Test",
		"-c", "user.email=test@example.com", "-c", "commit.gpgsign=false", "-c", "core.autocrlf=false"}, args...)
	cmd := exec.Command("git", full...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull)
	if date != "" {
		cmd.Env = append(cmd.Env, "GIT_AUTHOR_DATE="+date, "GIT_COMMITTER_DATE="+date)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func write(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func commit(t *testing.T, root, date, msg string, files map[string]string) {
	t.Helper()
	write(t, root, files)
	git(t, root, "", "add", "-A")
	git(t, root, date, "commit", "-q", "-m", msg)
}

// fixtureRepo builds a repository with one commit before the window and
// three inside it, plus an untracked unowned file.
func fixtureRepo(t *testing.T) (root, base string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root = t.TempDir()
	git(t, root, "", "init", "-q")
	commit(t, root, "2026-01-01T10:00:00Z", "feat: add alpha", map[string]string{
		".arch/registry.yaml":       testRegistry,
		".arch/baseline/lint.txt":   "x.go: bad\n",
		"internal/a/a.go":           "package a\n\nfunc A() {}\n",
		"internal/a/a_test.go":      "package a\n\n\n\n\n",
		"internal/a/legacy.go":      "package a\n",
		"internal/a/testdata/x.txt": "1\n2\n",
	})
	base = git(t, root, "", "rev-parse", "HEAD")
	commit(t, root, "2026-01-10T10:00:00Z", "fix(alpha): handle nil", map[string]string{
		"internal/a/a.go": "package a\n\nfunc A() {\n\t_ = 1\n}\n",
		"internal/b/b.go": "package b\n",
	})
	commit(t, root, "2026-01-11T10:00:00Z", "feat!: beta", map[string]string{
		"internal/b/b.go": "package b\n\nfunc B() {}\n",
	})
	commit(t, root, "2026-01-12T10:00:00Z", "fix: again", map[string]string{
		"internal/a/a.go": "package a\n\nfunc A() {\n}\n",
	})
	write(t, root, map[string]string{"other/x.go": "package other\n"})
	return root, base
}

func buildFixture(t *testing.T, since string, status func(string) (string, error)) (*Report, string) {
	t.Helper()
	root, base := fixtureRepo(t)
	reg, err := registry.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	files, err := registry.RepoFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := Build(Options{Root: root, Reg: reg, Files: files, Now: testNow, Since: since,
		BeadStatus: status, BeadsNotice: "beads: bd not found on PATH; bead features skipped"})
	if err != nil {
		t.Fatal(err)
	}
	return rep, base
}

func TestBuildReport(t *testing.T) {
	status := func(id string) (string, error) {
		if id == "t-1" {
			return "closed", nil
		}
		return "open", nil
	}
	rep, base := buildFixture(t, "", status)

	if rep.Since != "7d" || rep.From != "2026-01-08T12:00:00Z" || rep.To != "2026-01-15T12:00:00Z" || rep.Base != base {
		t.Errorf("window: since %q from %q to %q base %q (want %q)", rep.Since, rep.From, rep.To, rep.Base, base)
	}
	wantLOC := []ConceptLOC{{"alpha", 4, 5, 1}, {"beta", 0, 3, 3}}
	if !reflect.DeepEqual(rep.Concepts, wantLOC) {
		t.Errorf("concepts = %+v, want %+v", rep.Concepts, wantLOC)
	}
	h := rep.History
	if h.Commits != 3 || h.Fix != 2 || h.Feat != 1 || h.FixFeatRatio == nil || *h.FixFeatRatio != 2 {
		t.Errorf("history = %+v", h)
	}
	if h.Added != 3+1+2 || h.Deleted != 1+1 {
		t.Errorf("lines +%d -%d", h.Added, h.Deleted)
	}
	wantHot := []Hotspot{{"internal/a/a.go", 2}, {"internal/b/b.go", 1}}
	if !reflect.DeepEqual(rep.Hotspots, wantHot) {
		t.Errorf("hotspots = %+v", rep.Hotspots)
	}
	if !reflect.DeepEqual(rep.Unowned, []string{"other/x.go"}) {
		t.Errorf("unowned = %v", rep.Unowned)
	}
	if !reflect.DeepEqual(rep.Drafts, []string{"beta"}) ||
		!reflect.DeepEqual(rep.MultiStorage, []ConceptStorage{{"beta", []string{"t1", "t2"}}}) {
		t.Errorf("drafts %v multi %v", rep.Drafts, rep.MultiStorage)
	}
	if !reflect.DeepEqual(rep.NoReason, []string{"alpha"}) {
		t.Errorf("concepts without reason = %v (drafts are excluded)", rep.NoReason)
	}
	if len(rep.ExpiredFlags) != 1 || rep.ExpiredFlags[0].Name != "OLD_FLAG" {
		t.Errorf("expired flags = %+v", rep.ExpiredFlags)
	}
	if len(rep.OverdueRemovals) != 1 || rep.OverdueRemovals[0].ID != "old-a" {
		t.Errorf("overdue removals = %+v", rep.OverdueRemovals)
	}
	if len(rep.ClosedBeadRemovals) != 1 || rep.ClosedBeadRemovals[0].ID != "old-a" {
		t.Errorf("closed-bead removals = %+v", rep.ClosedBeadRemovals)
	}
	if len(rep.Checks) != 2 {
		t.Fatalf("checks = %+v", rep.Checks)
	}
	lint := rep.Checks[0]
	if lint.ID != "checks.lint" || lint.Findings != 2 || lint.Baselined != 1 || !reflect.DeepEqual(lint.New, []string{"y.go:2: worse"}) {
		t.Errorf("lint = %+v", lint)
	}
	if rep.Checks[1].Error == "" || !strings.Contains(rep.Checks[1].Error, "not found") {
		t.Errorf("missing check = %+v", rep.Checks[1])
	}
	if len(rep.Notices) != 0 {
		t.Errorf("notices = %v", rep.Notices)
	}

	text := rep.Text()
	for _, want := range []string{
		"since 7d; base " + base[:12],
		"alpha      4 ->      5  (+1)",
		"fix: 2, feat: 1 (fix/feat 2.00)",
		"    2  internal/a/a.go",
		"Unowned files: 1\n  other/x.go",
		"Concepts without a reason: 1\n  alpha",
		"Expired flags: 1\n  OLD_FLAG (alpha) expired 2026-01-10 [bead t-9]",
		"Overdue removals: 1\n  old-a (legacy a) due 2026-01-14, replaced by alpha [bead t-1]",
		"checks.lint: 2 finding(s), 1 baselined, 1 new\n    y.go:2: worse",
		"checks.missing: error:",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("text missing %q:\n%s", want, text)
		}
	}
}

func TestBuildDeterministic(t *testing.T) {
	a, _ := buildFixture(t, "1w", nil)
	b, _ := buildFixture(t, "1w", nil)
	if a.Text() != b.Text() {
		t.Errorf("reports differ:\n%s\n---\n%s", a.Text(), b.Text())
	}
}

func TestBuildWithoutBeadsAddsNotice(t *testing.T) {
	rep, _ := buildFixture(t, "7d", nil)
	if len(rep.ClosedBeadRemovals) != 0 || len(rep.Notices) != 1 || !strings.Contains(rep.Notices[0], "bead features skipped") {
		t.Errorf("closed %v notices %v", rep.ClosedBeadRemovals, rep.Notices)
	}
}

func TestBuildBeadErrorIsNotice(t *testing.T) {
	rep, _ := buildFixture(t, "7d", func(id string) (string, error) { return "", errors.New("bead not found") })
	if len(rep.Notices) != 2 || !strings.Contains(rep.Notices[0], "removal old-a: bead not found") {
		t.Errorf("notices %v", rep.Notices)
	}
}

func TestBuildWindowBeforeHistory(t *testing.T) {
	rep, _ := buildFixture(t, "30d", nil)
	if rep.Base != "" || rep.History.Commits != 4 || rep.Concepts[0].Before != 0 {
		t.Errorf("base %q commits %d concepts %+v", rep.Base, rep.History.Commits, rep.Concepts)
	}
	if !strings.Contains(rep.Text(), "no commit before the window") {
		t.Error(rep.Text())
	}
}

func TestBuildNoCommits(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	git(t, root, "", "init", "-q")
	reg, err := registry.Parse([]byte(testRegistry))
	if err != nil {
		t.Fatal(err)
	}
	reg.Checks = nil
	rep, err := Build(Options{Root: root, Reg: reg, Now: testNow})
	if err != nil {
		t.Fatal(err)
	}
	if rep.History.Commits != 0 || rep.History.FixFeatRatio != nil || len(rep.Concepts) != 2 {
		t.Errorf("report %+v", rep)
	}
}

func TestParseSince(t *testing.T) {
	for in, want := range map[string]int{"7d": 7, "1d": 1, "2w": 14, "30d": 30} {
		if got, err := ParseSince(in); err != nil || got != want {
			t.Errorf("ParseSince(%q) = %d, %v", in, got, err)
		}
	}
	for _, in := range []string{"", "0d", "7", "d", "1m", "-1d", "7 d", "1.5w"} {
		if _, err := ParseSince(in); err == nil {
			t.Errorf("ParseSince(%q) accepted", in)
		}
	}
}

func TestIsTest(t *testing.T) {
	for p, want := range map[string]bool{
		"internal/a/a_test.go":       true,
		"internal/a/a.go":            false,
		"pkg/test_x.py":              true,
		"pkg/x_test.py":              true,
		"pkg/x.py":                   false,
		"web/app.test.ts":            true,
		"web/app.spec.jsx":           true,
		"web/app.ts":                 false,
		"testdata/x.go":              true,
		"internal/tests/helper.py":   true,
		"src/__tests__/a.js":         true,
		"internal/testing/helper.go": false,
		"latest/x.go":                false,
	} {
		if got := IsTest(p); got != want {
			t.Errorf("IsTest(%q) = %v", p, got)
		}
	}
}
