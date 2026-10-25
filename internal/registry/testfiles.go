package registry

import (
	"path"
	"strings"
)

// testDirs are directory names whose contents count as tests.
var testDirs = map[string]bool{"test": true, "tests": true, "testdata": true, "__tests__": true}

// IsTest reports whether a repo-relative path is test code or test data,
// by common naming conventions (Go, Python, JS/TS) and test directories.
func IsTest(p string) bool {
	dir, base := path.Split(p)
	for _, seg := range strings.Split(strings.Trim(dir, "/"), "/") {
		if testDirs[seg] {
			return true
		}
	}
	stem := strings.TrimSuffix(base, path.Ext(base))
	switch path.Ext(base) {
	case ".go":
		return strings.HasSuffix(stem, "_test")
	case ".py":
		return strings.HasPrefix(stem, "test_") || strings.HasSuffix(stem, "_test")
	case ".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs", ".mts", ".cts":
		return strings.HasSuffix(stem, ".test") || strings.HasSuffix(stem, ".spec")
	}
	return false
}
