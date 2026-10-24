package gitx

import (
	"os"
	"path/filepath"
	"strings"
)

// Rel returns path relative to root, slash-separated, and whether it lies
// inside root. Symlinks are resolved on the deepest existing ancestor of
// path, so a path reached through a symlinked directory, or one not yet
// created, still compares correctly with the resolved root git reports.
func Rel(root, path string) (string, bool) {
	if r, err := filepath.EvalSymlinks(root); err == nil {
		root = r
	}
	path = resolveExisting(filepath.Clean(path))
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

func resolveExisting(p string) string {
	var rest []string
	for {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			return filepath.Join(append([]string{r}, rest...)...)
		} else if !os.IsNotExist(err) {
			return filepath.Join(append([]string{p}, rest...)...)
		}
		parent := filepath.Dir(p)
		if parent == p {
			return filepath.Join(append([]string{p}, rest...)...)
		}
		rest = append([]string{filepath.Base(p)}, rest...)
		p = parent
	}
}
