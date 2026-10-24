package gitx

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestHistory(t *testing.T) {
	root := newRepo(t)
	if base, err := CommitBefore(root, time.Now()); err != nil || base != "" {
		t.Fatalf("no commits: %q, %v", base, err)
	}
	if cs, err := Log(root, ""); err != nil || cs != nil {
		t.Fatalf("no commits: %v, %v", cs, err)
	}
	write(t, root, "a.txt", "1\n2\n")
	write(t, root, "bin.dat", "\x00\x01")
	if err := os.Symlink("a.txt", filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	commitAll(t, root, "feat: one")
	first := git(t, root, "rev-parse", "HEAD")[:40]
	write(t, root, "a.txt", "1\n3\n4\n")
	write(t, root, "dir/b c.txt", "x")
	commitAll(t, root, "fix(a): two")

	files, err := TreeFiles(root, first)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]byte{"a.txt": []byte("1\n2\n"), "bin.dat": []byte("\x00\x01")}
	if !reflect.DeepEqual(files, want) {
		t.Errorf("TreeFiles = %q", files)
	}
	cs, err := Log(root, first)
	if err != nil {
		t.Fatal(err)
	}
	wantLog := []Commit{{Hash: cs[0].Hash, Subject: "fix(a): two", Files: []NumStat{
		{Path: "a.txt", Added: 2, Deleted: 1}, {Path: "dir/b c.txt", Added: 1}}}}
	if !reflect.DeepEqual(cs, wantLog) {
		t.Errorf("Log = %+v", cs)
	}
	all, err := Log(root, "")
	if err != nil || len(all) != 2 || !all[1].Files[1].Binary {
		t.Errorf("Log all = %+v, %v", all, err)
	}
	if base, err := CommitBefore(root, time.Now().Add(time.Hour)); err != nil || len(base) != 40 {
		t.Errorf("CommitBefore = %q, %v", base, err)
	}
}
