package hook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func shell(root, session string) Event {
	return Event{Kind: PostShell, SessionID: session, Cwd: root, Command: "python3 - <<'EOF' ... EOF"}
}

func age(t *testing.T, root, rel string, d time.Duration) {
	t.Helper()
	old := time.Now().Add(-d)
	if err := os.Chtimes(filepath.Join(root, rel), old, old); err != nil {
		t.Fatal(err)
	}
}

func TestShellUnownedNewFileReportedOnce(t *testing.T) {
	root := newRepo(t)
	write(t, root, "internal/foo/x.go", "package foo\n")
	d := Decide(shell(root, "s1"), now)
	if d.Deny || d.Warning != "" || !strings.Contains(d.Feedback, "internal/foo/x.go: unowned") ||
		!strings.Contains(d.Feedback, "register an owner") {
		t.Fatalf("got %+v", d)
	}
	// Seen already, even once rewritten.
	write(t, root, "internal/foo/x.go", "package foo\n\nvar Y = 2\n")
	if d := Decide(shell(root, "s1"), now); d != (Decision{}) {
		t.Fatalf("second: %+v", d)
	}
	// An unowned file the command merely modified (it existed before) is
	// allowed, as in the pre-write hook.
	write(t, root, "legacy/old.go", "package legacy\n\nvar Z = 3\n")
	if d := Decide(shell(root, "s1"), now); d != (Decision{}) {
		t.Fatalf("modified unowned: %+v", d)
	}
	// A staged new file counts as new too.
	write(t, root, "other/y.go", "package other\n")
	git(t, root, "add", "other/y.go")
	if d := Decide(shell(root, "s1"), now); !strings.Contains(d.Feedback, "other/y.go: unowned") ||
		strings.Contains(d.Feedback, "internal/foo/x.go") {
		t.Fatalf("staged: %+v", d)
	}
}

func TestShellBriefOncePerConceptShared(t *testing.T) {
	root := newRepo(t)
	write(t, root, "internal/goal/goal.go", "package goal\n\n// NewGoal makes a goal.\nfunc NewGoal() {}\n")
	d := Decide(shell(root, "s1"), now)
	if d.Feedback != "" || !strings.Contains(d.Context, "the code your last command changed") ||
		!strings.Contains(d.Context, "Goals are work nodes.") {
		t.Fatalf("first: %+v", d)
	}
	// Further changes in a briefed concept add nothing, and the pre-write
	// hook shares the once-per-concept state.
	write(t, root, "internal/goal/goal.go", "package goal\n\n// NewGoal makes a goal.\nfunc NewGoal() {}\n\nvar A = 1\n")
	if d := Decide(shell(root, "s1"), now); d != (Decision{}) {
		t.Fatalf("again: %+v", d)
	}
	if d := Decide(pre(root, "s1", "internal/goal/goal.go"), now); d.Context != "" {
		t.Fatalf("pre-write after shell brief: %+v", d)
	}
	// A new file in the briefed concept repeats its provides, once.
	write(t, root, "internal/goal/other.go", "package goal\n")
	d = Decide(shell(root, "s1"), now)
	if !strings.Contains(d.Context, "creating a new file") || !strings.Contains(d.Context, "NewGoal") ||
		strings.Contains(d.Context, "Goals are work nodes.") {
		t.Fatalf("new file: %+v", d)
	}
	write(t, root, "internal/goal/other.go", "package goal\n\nvar B = 2\n")
	if d := Decide(shell(root, "s1"), now); d != (Decision{}) {
		t.Fatalf("new file again: %+v", d)
	}
}

func TestShellSkipsFilesAnnouncedByPreWrite(t *testing.T) {
	root := newRepo(t)
	if d := Decide(pre(root, "s1", "internal/goal/new.go"), now); d.Context == "" {
		t.Fatalf("pre-write: %+v", d)
	}
	write(t, root, "internal/goal/new.go", "package goal\n")
	if d := Decide(shell(root, "s1"), now); d != (Decision{}) {
		t.Fatalf("file created by the write tool was announced already: %+v", d)
	}
}

func TestShellFirstCallIgnoresOldDirtyFiles(t *testing.T) {
	root := newRepo(t)
	write(t, root, "scratch/old.txt", "mine\n")
	write(t, root, "internal/goal/goal.go", "package goal\n\nvar Old = 1\n")
	age(t, root, "scratch/old.txt", time.Hour)
	age(t, root, "internal/goal/goal.go", time.Hour)
	if d := Decide(shell(root, "s1"), now); d != (Decision{}) {
		t.Fatalf("dirty before the session: %+v", d)
	}
	// They are in the snapshot now; a later command changing one is seen.
	write(t, root, "internal/goal/goal.go", "package goal\n\nvar Old = 2\n")
	if d := Decide(shell(root, "s1"), now); !strings.Contains(d.Context, "Goals are work nodes.") {
		t.Fatalf("changed after the snapshot: %+v", d)
	}
	// An old untracked unowned file is reported once the session changes
	// it: it is still new to the repository and will fail the gate.
	write(t, root, "scratch/old.txt", "mine, edited\n")
	if d := Decide(shell(root, "s1"), now); !strings.Contains(d.Feedback, "scratch/old.txt: unowned") {
		t.Fatalf("untracked unowned changed after the snapshot: %+v", d)
	}
}

func TestShellIgnoredDeletedAndNoSession(t *testing.T) {
	root := newRepo(t)
	write(t, root, "internal/foo/x_test.go", "package foo\n") // ignored by the registry
	os.Remove(filepath.Join(root, "legacy/old.go"))
	if d := Decide(shell(root, "s1"), now); d != (Decision{}) {
		t.Fatalf("got %+v", d)
	}
	// Without a session id nothing persists: each call judges files by age.
	write(t, root, "internal/foo/x.go", "package foo\n")
	for i := 0; i < 2; i++ {
		if d := Decide(shell(root, ""), now); !strings.Contains(d.Feedback, "internal/foo/x.go") {
			t.Fatalf("call %d: %+v", i, d)
		}
	}
	if m, _ := filepath.Glob(filepath.Join(root, ".arch/cache/*")); len(m) != 1 {
		t.Fatalf("only the s1 state may exist: %v", m)
	}
}

func TestShellCapsUnownedList(t *testing.T) {
	root := newRepo(t)
	for i := 0; i < maxUnownedListed+3; i++ {
		write(t, root, "gen/f"+string(rune('a'+i))+".txt", "x\n")
	}
	d := Decide(shell(root, "s1"), now)
	if strings.Count(d.Feedback, ": unowned") != maxUnownedListed || !strings.Contains(d.Feedback, "and 3 more") {
		t.Fatalf("got %q", d.Feedback)
	}
}

func TestShellStateErrorsWarnOnly(t *testing.T) {
	root := newRepo(t)
	write(t, root, ".arch/cache", "not a directory")
	write(t, root, "internal/foo/x.go", "package foo\n")
	d := Decide(shell(root, "s1"), now)
	if d.Deny || d.Warning == "" || !strings.Contains(d.Feedback, "internal/foo/x.go") {
		t.Fatalf("got %+v", d)
	}
	// Outside a git repository: silent.
	if d := Decide(shell(t.TempDir(), "s1"), now); d != (Decision{}) {
		t.Fatalf("outside: %+v", d)
	}
}

func TestShellLegacyStateRead(t *testing.T) {
	root := newRepo(t)
	write(t, root, ".arch/cache/hook-s1.json", `["goal"]`)
	write(t, root, "internal/goal/goal.go", "package goal\n\nvar X = 1\n")
	if d := Decide(shell(root, "s1"), now); d != (Decision{}) {
		t.Fatalf("goal was briefed under the old state format: %+v", d)
	}
	if d := Decide(pre(root, "s1", "internal/goal/goal.go"), now); d.Context != "" {
		t.Fatalf("state rewritten without the concept: %+v", d)
	}
}

func TestShellWithBdCreateCombines(t *testing.T) {
	root := newRepo(t)
	state := fakeBd(t)
	fakeShowBead(t, state, "a-new", "Concepts touched: goal")
	write(t, root, "internal/goal/goal.go", "package goal\n\nvar X = 1\n")
	d := Decide(post(root, "bd create --silent x", "a-new\n"), now)
	if !strings.Contains(d.Context, "Goals are work nodes.") || !strings.Contains(d.Context, "a-new: arch:review") {
		t.Fatalf("got %+v", d)
	}
}

func TestShellModuleFeedback(t *testing.T) {
	root := moduleRepo(t)
	// A build writes an untracked generated file into the submodule: not an edit.
	write(t, root, "addons/Kit/a.cs.uid", "uid://generated\n")
	if d := Decide(shell(root, "s1"), now); strings.Contains(d.Feedback, "submodule") {
		t.Fatalf("untracked file in a submodule: %+v", d)
	}
	// Submodule edited from a shell: gitlink shows dirty.
	write(t, root, "addons/Kit/a.cs", "class A { int x; }\n")
	d := Decide(shell(root, "s1"), now)
	if !strings.Contains(d.Feedback, "module Kit: submodule has local changes; revert them in addons/Kit and use its public surface: Kit.Run.") {
		t.Fatalf("submodule: %+v", d)
	}
	// Reported once while unchanged.
	if d := Decide(shell(root, "s1"), now); d.Feedback != "" {
		t.Fatalf("repeat: %+v", d)
	}
	// Vendored file.
	write(t, root, "vendor/lib/x.go", "package lib\n\nvar X = 1\n")
	d = Decide(shell(root, "s1"), now)
	if !strings.Contains(d.Feedback, "vendor/lib/x.go is inside installed module lib; do not edit it.") ||
		!strings.HasSuffix(d.Feedback, "Revert the change.") {
		t.Fatalf("vendored: %+v", d)
	}
	// A plain new file in the catch-all concept is fine.
	write(t, root, "Player.cs", "class P {}\n")
	if d := Decide(shell(root, "s1"), now); d.Feedback != "" {
		t.Fatalf("plain: %+v", d)
	}
}

func TestShellModuleRegisteredByCommandGetsNoFeedback(t *testing.T) {
	root := moduleRepo(t)
	// Snapshot with no module "fresh" yet.
	Decide(shell(root, "s1"), now)
	// The command writes files and registers the module together.
	write(t, root, "third/fresh/f.go", "package fresh\n")
	write(t, root, ".arch/registry.yaml", moduleRegistry+`  - id: fresh
    paths: ["third/fresh/**"]
`)
	if d := Decide(shell(root, "s1"), now); d.Feedback != "" {
		t.Fatalf("registered by the command: %+v", d)
	}
	// Committed, then edited, it is a module.
	git(t, root, "add", "-A")
	git(t, root, "commit", "-q", "-m", "install fresh")
	write(t, root, "third/fresh/f.go", "package fresh\n\nvar Z = 1\n")
	if d := Decide(shell(root, "s1"), now); !strings.Contains(d.Feedback, "installed module fresh") {
		t.Fatalf("later edit: %+v", d)
	}
}

func TestShellModuleFirstCommandJudgedByHead(t *testing.T) {
	root := moduleRepo(t)
	// No snapshot: HEAD's registry knows the modules.
	write(t, root, "vendor/lib/x.go", "package lib\n\nvar X = 1\n")
	if d := Decide(shell(root, "s1"), now); !strings.Contains(d.Feedback, "installed module lib") {
		t.Fatalf("HEAD registry: %+v", d)
	}
}

// New files under a vendored module path are generated ones (Godot's
// *.uid and *.import), as in the gate; modified and deleted tracked files
// are edits.
func TestShellModuleNewFilesGetNoFeedback(t *testing.T) {
	root := moduleRepo(t)
	Decide(shell(root, "s1"), now) // snapshot
	write(t, root, "vendor/lib/x.go.uid", "uid://generated\n")
	write(t, root, "vendor/lib/icon.png.import", "[remap]\n")
	write(t, root, "vendor/lib/staged.go", "package lib\n")
	git(t, root, "add", "vendor/lib/staged.go")
	if d := Decide(shell(root, "s1"), now); d.Feedback != "" {
		t.Fatalf("new files: %+v", d)
	}
	os.Remove(filepath.Join(root, "vendor/lib/x.go"))
	d := Decide(shell(root, "s1"), now)
	if !strings.Contains(d.Feedback, "vendor/lib/x.go is inside installed module lib; do not edit it.") ||
		!strings.HasSuffix(d.Feedback, "Revert the change.") {
		t.Fatalf("deleted: %+v", d)
	}
	if d := Decide(shell(root, "s1"), now); d.Feedback != "" {
		t.Fatalf("deleted, repeat: %+v", d)
	}
}

// countSubmoduleGit counts the git commands moduleSubmodules runs.
func countSubmoduleGit(t *testing.T) *int {
	t.Helper()
	n := new(int)
	gl, sm := gitlinks, submoduleModified
	gitlinks = func(root string, dirs ...string) ([]string, error) { *n++; return gl(root, dirs...) }
	submoduleModified = func(dir string) (string, error) { *n++; return sm(dir) }
	t.Cleanup(func() { gitlinks, submoduleModified = gl, sm })
	return n
}

func TestShellNoModulesRunsNoSubmoduleGit(t *testing.T) {
	root := moduleRepo(t)
	write(t, root, ".arch/registry.yaml", testRegistry)
	git(t, root, "commit", "-q", "-am", "no modules")
	write(t, root, "addons/Kit/a.cs", "class A { int x; }\n")
	n := countSubmoduleGit(t)
	if d := Decide(shell(root, "s1"), now); strings.Contains(d.Feedback, "submodule") || *n != 0 {
		t.Fatalf("got %+v after %d submodule git commands", d, *n)
	}
}

func TestShellModulesCheckEachModuleSubmodule(t *testing.T) {
	root := moduleRepo(t)
	write(t, root, "addons/Kit/a.cs", "class A { int x; }\n")
	n := countSubmoduleGit(t)
	d := Decide(shell(root, "s1"), now)
	if !strings.Contains(d.Feedback, "module Kit: submodule has local changes") || *n != 2 {
		t.Fatalf("got %+v after %d git commands, want ls-files and one status", d, *n)
	}
}

func TestShellModulesListIndexOnlyUnderModulePaths(t *testing.T) {
	root := moduleRepo(t)
	var got [][]string
	gl := gitlinks
	gitlinks = func(r string, dirs ...string) ([]string, error) { got = append(got, dirs); return gl(r, dirs...) }
	t.Cleanup(func() { gitlinks = gl })
	write(t, root, "addons/Kit/a.cs", "class A { int x; }\n")
	Decide(shell(root, "s1"), now)
	if len(got) != 1 || strings.Join(got[0], ",") != "addons/Kit,vendor/lib" {
		t.Fatalf("ls-files scoped to %q, want addons/Kit and vendor/lib", got)
	}
}
