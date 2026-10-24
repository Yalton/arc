// Package imports extracts the imports of source files and resolves them to
// repository paths, for the gate's boundaries check (SPEC.md, "arc gate").
// Go resolves through `go list -json`; Python and TS/JS parse import lines.
package imports

import (
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Import is one import in a source file.
type Import struct {
	Path string // repo-relative file or package dir it resolves to, slash-separated; "" if external
	Raw  string // the import as written
	Line int    // 1-based line of the import (of its specifier, for multi-line statements)
}

// ReadFunc returns the content of a repo-relative file; ok is false when it
// is not a regular file or no longer exists.
type ReadFunc func(path string) (data []byte, ok bool, err error)

// Imports reads files (repo-relative, slash-separated) from the working tree
// and returns the imports of each one in a supported language. Files in other
// languages, or that do not exist, are absent from the result.
func Imports(root string, files []string) (map[string][]Import, error) {
	return ImportsWith(root, files, func(p string) ([]byte, bool, error) {
		fp := filepath.Join(root, filepath.FromSlash(p))
		fi, err := os.Lstat(fp)
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		if err != nil || !fi.Mode().IsRegular() {
			return nil, false, err
		}
		data, err := os.ReadFile(fp)
		return data, err == nil, err
	})
}

// ImportsWith is Imports with file contents taken from read (the gate uses
// it to judge staged content). Resolution still looks at the working tree.
func ImportsWith(root string, files []string, read ReadFunc) (map[string][]Import, error) {
	out := map[string][]Import{}
	var goFiles []string
	for _, f := range files {
		var parse func(root, file string, data []byte) []Import
		switch strings.ToLower(path.Ext(f)) {
		case ".go":
			goFiles = append(goFiles, f)
			continue
		case ".py":
			parse = pythonImports
		case ".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs", ".mts", ".cts":
			parse = jsImports
		default:
			continue
		}
		data, ok, err := read(f)
		if err != nil {
			return nil, err
		}
		if ok {
			out[f] = parse(root, f, data)
		}
	}
	if len(goFiles) > 0 {
		if err := goImports(root, goFiles, read, out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// isFile and isDir report whether a repo-relative path exists on disk as a
// regular file or a directory.
func isFile(root, rel string) bool {
	fi, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel)))
	return err == nil && fi.Mode().IsRegular()
}

func isDir(root, rel string) bool {
	fi, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel)))
	return err == nil && fi.IsDir()
}

// inRepo cleans a path joined from repo-relative parts; ok is false when it
// escapes the repository.
func inRepo(p string) (string, bool) {
	p = path.Clean(p)
	if p == ".." || strings.HasPrefix(p, "../") || path.IsAbs(p) {
		return "", false
	}
	return p, true
}

// lineAt returns the 1-based line of byte offset off in src.
func lineAt(src string, off int) int {
	return strings.Count(src[:off], "\n") + 1
}
