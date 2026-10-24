package gitx

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRelThroughSymlinkAndMissing(t *testing.T) {
	real := t.TempDir()
	os.MkdirAll(filepath.Join(real, "a"), 0o755)
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skip("symlinks unsupported:", err)
	}
	cases := map[string]string{
		filepath.Join(link, "a", "x.go"):          "a/x.go",
		filepath.Join(link, "new", "dir", "y.go"): "new/dir/y.go",
		filepath.Join(real, "a", "x.go"):          "a/x.go",
	}
	for p, want := range cases {
		if got, ok := Rel(real, p); !ok || got != want {
			t.Errorf("Rel(%s) = %q, %v; want %q", p, got, ok, want)
		}
	}
	if _, ok := Rel(real, filepath.Join(filepath.Dir(real), "elsewhere")); ok {
		t.Error("outside path reported inside")
	}
}
