package gitx

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// newRepo creates an empty git repository with a hermetic configuration.
func newRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	// Resolve symlinks (e.g. macOS /var -> /private/var) so Root comparisons hold.
	dir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	git(t, dir, "init", "-q")
	return dir
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-C", dir,
		"-c", "init.defaultBranch=main",
		"-c", "user.name=Test",
		"-c", "user.email=test@example.com",
		"-c", "commit.gpgsign=false",
		"-c", "core.autocrlf=false",
	}, args...)
	cmd := exec.Command("git", full...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func commitAll(t *testing.T, root, msg string) {
	t.Helper()
	git(t, root, "add", "-A")
	git(t, root, "commit", "-q", "-m", msg)
}

func lines(pairs ...any) []Line {
	var out []Line
	for i := 0; i < len(pairs); i += 2 {
		out = append(out, Line{Num: pairs[i].(int), Text: pairs[i+1].(string)})
	}
	return out
}

func wantFile(t *testing.T, cs *ChangeSet, path string, st Status, added []Line) {
	t.Helper()
	f := cs.File(path)
	if f == nil {
		t.Fatalf("%s: not in change set %+v", path, cs.Files)
	}
	if f.Status != st {
		t.Errorf("%s: status %v, want %v", path, f.Status, st)
	}
	if len(f.Added) == 0 && len(added) == 0 {
		return
	}
	if !reflect.DeepEqual(f.Added, added) {
		t.Errorf("%s: added lines\n got  %+v\n want %+v", path, f.Added, added)
	}
}

func TestBaseChangeSet(t *testing.T) {
	r := newRepo(t)
	write(t, r, "keep.txt", "a\nb\nc\nd\n")
	write(t, r, "gone.txt", "bye\n")
	write(t, r, "old name.txt", "renamed\n")
	write(t, r, "bin.dat", "\x00\x01\x02")
	commitAll(t, r, "base")

	git(t, r, "checkout", "-q", "-b", "feature")
	write(t, r, "keep.txt", "a\nB\nc\nd\ne\nf") // modify line 2, append 5-6, no trailing newline
	write(t, r, "dir/new file.go", "package x\n\nfunc F() {}\n")
	write(t, r, "bin.dat", "\x00\x09\x09\x09")
	write(t, r, "empty.txt", "")
	write(t, r, "+++ odd.txt", "+++ b/x\n--- a/y\n@@ -1 +1 @@\n")
	if err := os.Remove(filepath.Join(r, "gone.txt")); err != nil {
		t.Fatal(err)
	}
	git(t, r, "mv", "old name.txt", "new name.txt")
	commitAll(t, r, "feature work")

	// A later commit on main must not appear: the diff starts at the merge base.
	git(t, r, "checkout", "-q", "main")
	write(t, r, "main-only.txt", "main\n")
	write(t, r, "keep.txt", "a\nb\nc\nd\nmain\n")
	commitAll(t, r, "main moves on")
	git(t, r, "checkout", "-q", "feature")

	cs, err := BaseChangeSet(r, "main")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := cs.Added(), []string{"+++ odd.txt", "dir/new file.go", "empty.txt", "new name.txt"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Added = %q, want %q", got, want)
	}
	if got, want := cs.Modified(), []string{"bin.dat", "keep.txt"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Modified = %q, want %q", got, want)
	}
	if got, want := cs.Deleted(), []string{"gone.txt", "old name.txt"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Deleted = %q, want %q", got, want)
	}
	if cs.File("main-only.txt") != nil {
		t.Error("main-only.txt leaked into change set past the merge base")
	}
	wantFile(t, cs, "keep.txt", Modified, lines(2, "B", 5, "e", 6, "f"))
	wantFile(t, cs, "dir/new file.go", Added, lines(1, "package x", 2, "", 3, "func F() {}"))
	wantFile(t, cs, "new name.txt", Added, lines(1, "renamed"))
	wantFile(t, cs, "+++ odd.txt", Added, lines(1, "+++ b/x", 2, "--- a/y", 3, "@@ -1 +1 @@"))
	wantFile(t, cs, "bin.dat", Modified, nil)
	wantFile(t, cs, "empty.txt", Added, nil)
	wantFile(t, cs, "gone.txt", Deleted, nil)

	for i := 1; i < len(cs.Files); i++ {
		if cs.Files[i-1].Path >= cs.Files[i].Path {
			t.Errorf("Files not sorted: %q before %q", cs.Files[i-1].Path, cs.Files[i].Path)
		}
	}

	if _, err := BaseChangeSet(r, "no-such-ref"); err == nil {
		t.Error("BaseChangeSet with unknown ref: want error")
	} else if !strings.Contains(err.Error(), "no-such-ref") {
		t.Errorf("error should include git stderr: %v", err)
	}
}

func TestBaseChangeSetEmpty(t *testing.T) {
	r := newRepo(t)
	write(t, r, "a.txt", "a\n")
	commitAll(t, r, "one")
	cs, err := BaseChangeSet(r, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if len(cs.Files) != 0 {
		t.Errorf("want empty change set, got %+v", cs.Files)
	}
}

func TestStagedChangeSet(t *testing.T) {
	r := newRepo(t)
	write(t, r, "a.txt", "1\n2\n3\n")
	write(t, r, "b.txt", "b\n")
	commitAll(t, r, "init")

	write(t, r, "a.txt", "1\n2\n3\n4\n")
	write(t, r, "c.txt", "new\n")
	git(t, r, "add", "a.txt", "c.txt")
	git(t, r, "rm", "-q", "b.txt")
	// Unstaged edits and untracked files must not count.
	write(t, r, "a.txt", "1\n2\n3\n4\nunstaged\n")
	write(t, r, "untracked.txt", "x\n")

	cs, err := StagedChangeSet(r)
	if err != nil {
		t.Fatal(err)
	}
	if len(cs.Files) != 3 {
		t.Fatalf("want 3 files, got %+v", cs.Files)
	}
	wantFile(t, cs, "a.txt", Modified, lines(4, "4"))
	wantFile(t, cs, "c.txt", Added, lines(1, "new"))
	wantFile(t, cs, "b.txt", Deleted, nil)
}

func TestStagedChangeSetNoCommits(t *testing.T) {
	r := newRepo(t)
	write(t, r, "x/first.go", "package x\n")
	write(t, r, "pic.bin", "\x00\xff\x00")
	git(t, r, "add", "-A")

	cs, err := StagedChangeSet(r)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := cs.Added(), []string{"pic.bin", "x/first.go"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Added = %q, want %q", got, want)
	}
	wantFile(t, cs, "x/first.go", Added, lines(1, "package x"))
	wantFile(t, cs, "pic.bin", Added, nil)

	// Nothing staged at all.
	empty := newRepo(t)
	cs, err = StagedChangeSet(empty)
	if err != nil {
		t.Fatal(err)
	}
	if len(cs.Files) != 0 {
		t.Errorf("want empty change set, got %+v", cs.Files)
	}
}

func TestRoot(t *testing.T) {
	r := newRepo(t)
	write(t, r, "a/b/c.txt", "x\n")
	got, err := Root(filepath.Join(r, "a", "b"))
	if err != nil {
		t.Fatal(err)
	}
	if got != r {
		t.Errorf("Root = %q, want %q", got, r)
	}
	if _, err := Root(t.TempDir()); err == nil {
		t.Error("Root outside a repository: want error")
	}
}

func TestTrackedFilesAndFiles(t *testing.T) {
	r := newRepo(t)
	write(t, r, ".gitignore", "*.log\n")
	write(t, r, "z.txt", "z\n")
	write(t, r, "dir/with space.txt", "s\n")
	write(t, r, "doomed.txt", "d\n")
	commitAll(t, r, "init")

	write(t, r, "new/untracked.go", "package n\n")
	write(t, r, "ignored.log", "noise\n")
	write(t, r, "staged.txt", "s\n")
	git(t, r, "add", "staged.txt")
	if err := os.Remove(filepath.Join(r, "doomed.txt")); err != nil {
		t.Fatal(err)
	}
	write(t, r, "z.txt", "changed\n") // modified in worktree: listed once

	tracked, err := TrackedFiles(r)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{".gitignore", "dir/with space.txt", "doomed.txt", "staged.txt", "z.txt"}; !reflect.DeepEqual(tracked, want) {
		t.Errorf("TrackedFiles = %q, want %q", tracked, want)
	}

	files, err := Files(r)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{".gitignore", "dir/with space.txt", "new/untracked.go", "staged.txt", "z.txt"}; !reflect.DeepEqual(files, want) {
		t.Errorf("Files = %q, want %q", files, want)
	}
}

func TestParsePatchNoNewline(t *testing.T) {
	patch := "diff --git a/f b/f\n--- a/f\n+++ b/f\n@@ -2 +2,2 @@\n-old\n\\ No newline at end of file\n+new\n+last\n\\ No newline at end of file\n" +
		"diff --git a/g h b/g h\nnew file mode 100644\n--- /dev/null\n+++ b/g h\t\n@@ -0,0 +1 @@\n+hi\n" +
		"diff --git a/q b/q\n--- a/q\n+++ \"b/tab\\there\"\n@@ -5,0 +6 @@\n+x\n"
	got, err := parsePatch([]byte(patch))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]Line{
		"f":         lines(2, "new", 3, "last"),
		"g h":       lines(1, "hi"),
		"tab\there": lines(6, "x"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parsePatch =\n %+v\nwant\n %+v", got, want)
	}
}

func TestWorkingChangeSet(t *testing.T) {
	root := t.TempDir()
	g := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root, "-c", "user.name=t", "-c", "user.email=t@t",
			"-c", "init.defaultBranch=main", "-c", "commit.gpgsign=false"}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	w := func(p, c string) {
		os.MkdirAll(filepath.Join(root, filepath.Dir(p)), 0o755)
		os.WriteFile(filepath.Join(root, p), []byte(c), 0o644)
	}
	g("init", "-q")
	// No commits yet: untracked files still count.
	w("new.go", "a\nb\n")
	cs, err := WorkingChangeSet(root)
	if err != nil || len(cs.Added()) != 1 {
		t.Fatalf("no-commit repo: %v %v", cs, err)
	}
	w("committed.go", "one\n")
	w("gone.go", "x\n")
	w(".gitignore", "ignored.txt\n")
	g("add", "committed.go", "gone.go", ".gitignore")
	g("commit", "-q", "-m", "init")
	w("committed.go", "one\ntwo\n") // unstaged edit
	w("staged.go", "s\n")
	g("add", "staged.go") // staged add
	os.Remove(filepath.Join(root, "gone.go"))
	w("ignored.txt", "i\n")
	cs, err = WorkingChangeSet(root)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(cs.Added(), ",") + "|" + strings.Join(cs.Modified(), ",") + "|" + strings.Join(cs.Deleted(), ",")
	if got != "new.go,staged.go|committed.go|gone.go" {
		t.Fatalf("got %s", got)
	}
	if l := cs.File("committed.go").Added; len(l) != 1 || l[0].Num != 2 || l[0].Text != "two" {
		t.Errorf("committed.go added = %+v", l)
	}
	if l := cs.File("new.go").Added; len(l) != 2 || l[1].Text != "b" {
		t.Errorf("new.go added = %+v", l)
	}
}
