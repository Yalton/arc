package registry

import (
	"io/fs"
	"path/filepath"
	"sort"

	"arc/internal/gitx"
)

// RepoFiles lists the files validation and ownership are judged against:
// tracked plus untracked-not-ignored files in a git repository, or every
// regular file under root (skipping .git) outside one.
func RepoFiles(root string) ([]string, error) {
	if _, err := gitx.Root(root); err == nil {
		return gitx.Files(root)
	}
	var files []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		if d.Type().IsRegular() {
			rel, err := filepath.Rel(root, p)
			if err != nil {
				return err
			}
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	sort.Strings(files)
	return files, err
}
