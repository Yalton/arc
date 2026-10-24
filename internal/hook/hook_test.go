package hook

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var now = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

const testRegistry = `version: 1
system: {name: demo, summary: Demo.}
ignore: ["**/*_test.go"]
concepts:
  - id: goal
    name: Goal
    summary: Operator goals.
    paths: ["internal/goal/**"]
    rules: [Goals are work nodes.]
`

// newRepo creates a git repo with a registry and one owned file.
func newRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	cmd := exec.Command("git", "init", "-q", root)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	write(t, root, ".arch/registry.yaml", testRegistry)
	write(t, root, "internal/goal/goal.go", "package goal\n")
	write(t, root, "legacy/old.go", "package legacy\n")
	// Resolve symlinks (e.g. /tmp on macOS) so paths match git's top-level.
	r, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func pre(root, session string, paths ...string) Event {
	return Event{Kind: PreWrite, SessionID: session, Cwd: root, Paths: paths}
}

func TestDecideUnownedNewFileDenied(t *testing.T) {
	root := newRepo(t)
	d := Decide(pre(root, "s1", "internal/foo/x.go"), now)
	if !d.Deny || !strings.Contains(d.Reason, "internal/foo/x.go: unowned") {
		t.Fatalf("got %+v", d)
	}
}

func TestDecideUnownedExistingFileAllowed(t *testing.T) {
	root := newRepo(t)
	if d := Decide(pre(root, "s1", filepath.Join(root, "legacy/old.go")), now); d.Deny || d.Warning != "" {
		t.Fatalf("editing a pre-existing unowned file must be allowed: %+v", d)
	}
}

func TestDecideOwnedContextOncePerSession(t *testing.T) {
	root := newRepo(t)
	d := Decide(pre(root, "s1", "internal/goal/new.go"), now)
	if d.Deny || !strings.Contains(d.Context, "Goals are work nodes.") {
		t.Fatalf("first: %+v", d)
	}
	if d := Decide(pre(root, "s1", "internal/goal/goal.go"), now); d.Context != "" || d.Deny {
		t.Fatalf("second in same session must have no context: %+v", d)
	}
	if d := Decide(pre(root, "s2", "internal/goal/goal.go"), now); d.Context == "" {
		t.Fatal("new session must get context again")
	}
}

func TestDecideIgnoredAndOutside(t *testing.T) {
	root := newRepo(t)
	if d := Decide(pre(root, "s1", "internal/foo/x_test.go"), now); d != (Decision{}) {
		t.Fatalf("ignored: %+v", d)
	}
	if d := Decide(pre(root, "s1", "/nonexistent-arc-dir/x.go"), now); d != (Decision{}) {
		t.Fatalf("outside: %+v", d)
	}
}

func TestDecideNoRegistryAllows(t *testing.T) {
	root := newRepo(t)
	os.Remove(filepath.Join(root, ".arch/registry.yaml"))
	if d := Decide(pre(root, "s1", "new/x.go"), now); d != (Decision{}) {
		t.Fatalf("got %+v", d)
	}
}

func TestDecideBrokenRegistryWarnsAndAllows(t *testing.T) {
	root := newRepo(t)
	write(t, root, ".arch/registry.yaml", "version: [")
	d := Decide(pre(root, "s1", "new/x.go"), now)
	if d.Deny || d.Warning == "" {
		t.Fatalf("got %+v", d)
	}
}

func TestBdCreateMatch(t *testing.T) {
	for cmd, want := range map[string]bool{
		`bd create "x" -p 1`:          true,
		`cd a && bd create --title=x`: true,
		`echo bd created`:             false,
		`abd create`:                  false,
	} {
		if got := bdCreate.MatchString(cmd); got != want {
			t.Errorf("%q: got %v", cmd, got)
		}
	}
}
