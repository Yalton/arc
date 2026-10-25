// Package gitx shells out to git to discover the repository root, list
// files, and compute the change sets judged by arc gate.
package gitx

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// run executes git with -C root and returns stdout. Errors include stderr.
func run(root string, stdin []byte, args ...string) ([]byte, error) {
	full := append([]string{"-C", root, "-c", "core.quotepath=false"}, args...)
	cmd := exec.Command("git", full...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return nil, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
		}
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, msg)
	}
	return stdout.Bytes(), nil
}

// Root returns the absolute repository top-level for dir (git rev-parse --show-toplevel).
func Root(dir string) (string, error) {
	out, err := run(dir, nil, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	root := strings.TrimRight(string(out), "\r\n")
	if root == "" {
		return "", fmt.Errorf("git rev-parse --show-toplevel in %s: empty output (bare repository?)", dir)
	}
	return filepath.Clean(filepath.FromSlash(root)), nil
}

// TrackedFiles returns repo-relative, slash-separated paths of tracked files (git ls-files -z), sorted.
func TrackedFiles(root string) ([]string, error) {
	out, err := run(root, nil, "ls-files", "-z", "--cached")
	if err != nil {
		return nil, err
	}
	return sortedUnique(splitZ(out), nil), nil
}

// Files returns tracked plus untracked-but-not-ignored files, deduplicated,
// sorted and slash-separated, excluding files deleted from the working tree.
func Files(root string) ([]string, error) {
	out, err := run(root, nil, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	del, err := run(root, nil, "ls-files", "-z", "--deleted")
	if err != nil {
		return nil, err
	}
	gone := make(map[string]bool)
	for _, p := range splitZ(del) {
		gone[p] = true
	}
	return sortedUnique(splitZ(out), gone), nil
}

func splitZ(b []byte) []string {
	var out []string
	for _, f := range bytes.Split(b, []byte{0}) {
		if len(f) > 0 {
			out = append(out, string(f))
		}
	}
	return out
}

func sortedUnique(in []string, exclude map[string]bool) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, p := range in {
		if seen[p] || exclude[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// StagedFile returns the index (staged) content of a regular file, and
// false for anything else (symlink, submodule, not in the index).
func StagedFile(root, path string) ([]byte, bool, error) {
	out, err := run(root, nil, "ls-files", "-s", "-z", "--", path)
	if err != nil {
		return nil, false, err
	}
	// "<mode> <object> <stage>\t<path>\x00"
	if !bytes.HasPrefix(out, []byte("100644 ")) && !bytes.HasPrefix(out, []byte("100755 ")) {
		return nil, false, nil
	}
	data, err := run(root, nil, "show", ":"+path)
	return data, err == nil, err
}

// FileAt returns path's content at rev, and false when rev does not exist
// or has no such file.
func FileAt(root, rev, path string) ([]byte, bool, error) {
	if _, err := run(root, nil, "cat-file", "-e", rev+":"+path); err != nil {
		return nil, false, nil
	}
	data, err := run(root, nil, "show", rev+":"+path)
	return data, err == nil, err
}

// MergeBase returns the merge-base commit of ref and HEAD.
func MergeBase(root, ref string) (string, error) {
	out, err := run(root, nil, "merge-base", ref, "HEAD")
	return strings.TrimSpace(string(out)), err
}

// DirtyPath is one 'git status' entry: a repo-relative, slash-separated
// path and its two-letter porcelain code ("??" untracked, "A " added,
// " M" modified, ...).
type DirtyPath struct {
	Path string
	Code string
}

// WorktreeStatus lists every path that differs from HEAD in the index or
// the working tree, untracked files included one by one and ignored files
// left out, with renames reported as an add plus a delete. It is one
// 'git status --porcelain -z' call and takes no optional locks, so it is
// safe to run while an agent's own git commands are in flight.
func WorktreeStatus(root string) ([]DirtyPath, error) {
	out, err := run(root, nil, "--no-optional-locks", "status", "--porcelain=v1", "-z",
		"--untracked-files=all", "--no-renames", "--ignore-submodules=all")
	if err != nil {
		return nil, err
	}
	var paths []DirtyPath
	for _, rec := range splitZ(out) {
		if len(rec) < 4 || rec[2] != ' ' {
			continue
		}
		paths = append(paths, DirtyPath{Path: rec[3:], Code: rec[:2]})
	}
	return paths, nil
}
