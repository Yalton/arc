package beads

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeBd returns a client running testdata/fakebd/bd with a fresh state
// directory, which it also returns.
func fakeBd(t *testing.T) (*Client, string) {
	t.Helper()
	bin, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fakebd", "bd"))
	if err != nil {
		t.Fatal(err)
	}
	state := t.TempDir()
	t.Setenv("FAKEBD_DIR", state)
	return &Client{Bin: bin, Root: state}, state
}

func writeState(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readState(t *testing.T, dir, name string) string {
	t.Helper()
	data, _ := os.ReadFile(filepath.Join(dir, name))
	return string(data)
}

// showJSON is 'bd show --json' output as printed by bd 1.3.0.
const showJSON = `[
  {
    "id": "tst-3xl",
    "title": "Add goal export",
    "description": "Replaces / removes: none\nConcepts touched: goal",
    "design": "design text",
    "acceptance_criteria": "must pass",
    "notes": "first note\nsecond",
    "status": "open",
    "priority": 2,
    "issue_type": "task",
    "created_at": "2026-10-24T18:36:33Z",
    "labels": ["arch:ok", "bar"],
    "dependent_count": 0,
    "revision": "8271317232504983751"
  }
]`

func TestShowParsesBd130Shape(t *testing.T) {
	c, state := fakeBd(t)
	writeState(t, state, "show-tst-3xl.json", showJSON)
	b, err := c.Show("tst-3xl")
	if err != nil {
		t.Fatal(err)
	}
	if b.ID != "tst-3xl" || b.Title != "Add goal export" || b.Design != "design text" ||
		b.AcceptanceCriteria != "must pass" || b.Notes != "first note\nsecond" || b.Status != "open" ||
		!b.HasLabel("arch:ok") || !strings.Contains(b.Description, "Concepts touched") {
		t.Fatalf("got %+v", b)
	}
	if ts, ok := b.Created(); !ok || !ts.Equal(time.Date(2026, 10, 24, 18, 36, 33, 0, time.UTC)) {
		t.Fatalf("created %v %v", ts, ok)
	}
}

func TestShowNotFoundIncludesStderr(t *testing.T) {
	c, _ := fakeBd(t)
	_, err := c.Show("tst-nope")
	if err == nil || !strings.Contains(err.Error(), "Issue tst-nope not found") {
		t.Fatalf("err %v", err)
	}
}

func TestShowRejectsFlagLikeID(t *testing.T) {
	c, state := fakeBd(t)
	if _, err := c.Show("--all"); err == nil {
		t.Fatal("want error")
	}
	if readState(t, state, "calls") != "" {
		t.Fatal("bd must not run for an invalid id")
	}
}

func TestListFiltersLabelPrefix(t *testing.T) {
	c, state := fakeBd(t)
	writeState(t, state, "list.json", `[
	  {"id": "a-1", "status": "open", "labels": ["arch:ok"]},
	  {"id": "a-2", "status": "open"},
	  {"id": "a-3", "status": "open", "labels": ["archive"]}
	]`)
	bs, err := c.List("open", "arch:")
	if err != nil {
		t.Fatal(err)
	}
	if len(bs) != 2 || bs[0].ID != "a-2" || bs[1].ID != "a-3" {
		t.Fatalf("got %+v", bs)
	}
	if calls := readState(t, state, "calls"); !strings.Contains(calls, "list --status=open --limit=0 --json") {
		t.Fatalf("calls %q", calls)
	}
}

func TestWritesUseExpectedCommands(t *testing.T) {
	c, state := fakeBd(t)
	writeState(t, state, "create.json", `{"id": "tst-new", "title": "T", "status": "open"}`)
	if err := c.AddLabel("tst-1", "arch:ok"); err != nil {
		t.Fatal(err)
	}
	if err := c.RemoveLabel("tst-1", "arch:review"); err != nil {
		t.Fatal(err)
	}
	if err := c.Note("tst-1", "-starts with a dash\nline 2"); err != nil {
		t.Fatal(err)
	}
	id, err := c.Create("T", "desc", []string{"x", "y"})
	if err != nil || id != "tst-new" {
		t.Fatalf("create: %q %v", id, err)
	}
	calls := readState(t, state, "calls")
	for _, want := range []string{
		"label add tst-1 arch:ok",
		"label remove tst-1 arch:review",
		"note tst-1 --stdin",
		"create --title=T --stdin --json --labels=x,y",
	} {
		if !strings.Contains(calls, want+"\n") {
			t.Errorf("missing call %q in %q", want, calls)
		}
	}
	if stdin := readState(t, state, "stdin"); !strings.Contains(stdin, "== note tst-1\n-starts with a dash\nline 2") ||
		!strings.Contains(stdin, "== create\ndesc") {
		t.Fatalf("stdin %q", stdin)
	}
}

func TestTimeout(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "bd")
	writeState(t, dir, "bd", "#!/bin/sh\nsleep 5\n")
	os.Chmod(bin, 0o755)
	c := &Client{Bin: bin, Root: dir, Timeout: 100 * time.Millisecond}
	if _, err := c.Show("x-1"); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err %v", err)
	}
}

func TestOpenWithoutBdOrWorkspace(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if c, notice := Open(t.TempDir()); c != nil || !strings.Contains(notice, "bd not found") {
		t.Fatalf("got %v %q", c, notice)
	}
	c, state := fakeBd(t)
	t.Setenv("PATH", filepath.Dir(c.Bin))
	writeState(t, state, "nows", "")
	if c, notice := Open(state); c != nil || !strings.Contains(notice, "'bd where' failed") {
		t.Fatalf("got %v %q", c, notice)
	}
	os.Remove(filepath.Join(state, "nows"))
	if c, notice := Open(state); c == nil || notice != "" {
		t.Fatalf("got %v %q", c, notice)
	}
}

// TestRealBd runs the wrapper against bd itself in a throwaway workspace.
func TestRealBd(t *testing.T) {
	if _, err := exec.LookPath("bd"); err != nil {
		t.Skip("bd not installed")
	}
	if testing.Short() {
		t.Skip("short mode")
	}
	// Never let an inherited workspace override the temp one.
	for _, k := range []string{"BEADS_DIR", "BEADS_DB"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
	root := t.TempDir()
	for _, args := range [][]string{
		{"git", "init", "-q"},
		{"bd", "init", "--stealth", "--non-interactive", "-p", "tst"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}
	c, notice := Open(root)
	if c == nil {
		t.Fatal(notice)
	}
	id, err := c.Create("Add goal export", "Concepts touched: goal\nline 2", []string{"foo"})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Note(id, "first"); err != nil {
		t.Fatal(err)
	}
	if err := c.Note(id, "second"); err != nil {
		t.Fatal(err)
	}
	if err := c.AddLabel(id, "arch:review"); err != nil {
		t.Fatal(err)
	}
	if err := c.AddLabel(id, "arch:ok"); err != nil {
		t.Fatal(err)
	}
	if err := c.RemoveLabel(id, "arch:review"); err != nil {
		t.Fatal(err)
	}
	b, err := c.Show(id)
	if err != nil {
		t.Fatal(err)
	}
	if b.Title != "Add goal export" || b.Description != "Concepts touched: goal\nline 2" ||
		!strings.Contains(b.Notes, "first") || !strings.Contains(b.Notes, "second") ||
		!b.HasLabel("arch:ok") || b.HasLabel("arch:review") || !b.HasLabel("foo") || b.Status != "open" {
		t.Fatalf("got %+v", b)
	}
	if ts, ok := b.Created(); !ok || time.Since(ts) > time.Hour {
		t.Fatalf("created_at %q", b.CreatedAt)
	}
	other, err := c.Create("Untriaged", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	bs, err := c.List("open", "arch:")
	if err != nil {
		t.Fatal(err)
	}
	if len(bs) != 1 || bs[0].ID != other {
		t.Fatalf("list: %+v", bs)
	}
	if _, err := c.Show("tst-doesnotexist"); err == nil {
		t.Fatal("want not-found error")
	}
}
