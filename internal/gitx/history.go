package gitx

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// History stats for arc audit (SPEC.md, "arc audit"). Old file contents
// are read from git objects; nothing is checked out.

// Commit is one non-merge commit with its per-file line counts.
type Commit struct {
	Hash    string
	Subject string
	Files   []NumStat
}

// NumStat is one file's 'git log --numstat' entry. Binary files have no
// line counts.
type NumStat struct {
	Path    string
	Added   int
	Deleted int
	Binary  bool
}

// HasHead reports whether the repository has at least one commit.
func HasHead(root string) bool {
	_, err := run(root, nil, "rev-parse", "--verify", "--quiet", "HEAD^{commit}")
	return err == nil
}

// CommitBefore returns the newest commit reachable from HEAD whose
// committer date is before t ('git rev-list -1 --before'), or "" when
// there is none (history starts after t, or no commits).
func CommitBefore(root string, t time.Time) (string, error) {
	if !HasHead(root) {
		return "", nil
	}
	out, err := run(root, nil, "rev-list", "-1", "--before="+t.UTC().Format(time.RFC3339), "HEAD")
	return strings.TrimSpace(string(out)), err
}

// Log returns the non-merge commits in base..HEAD, newest first, or every
// commit reachable from HEAD when base is "". Renames are counted as a
// delete plus an add.
func Log(root, base string) ([]Commit, error) {
	if !HasHead(root) {
		return nil, nil
	}
	rng := "HEAD"
	if base != "" {
		rng = base + "..HEAD"
	}
	out, err := run(root, nil, "log", "--no-merges", "--no-renames", "--no-color", "--numstat", "-z",
		"--format=%x01%H%x00%s%x00", rng, "--")
	if err != nil {
		return nil, err
	}
	var commits []Commit
	for _, rec := range strings.Split(string(out), "\x01") {
		if rec == "" {
			continue
		}
		fields := strings.Split(rec, "\x00")
		if len(fields) < 2 {
			return nil, fmt.Errorf("git log: malformed record %q", rec)
		}
		c := Commit{Hash: fields[0], Subject: fields[1]}
		for _, f := range fields[2:] {
			f = strings.TrimLeft(f, "\n")
			if f == "" {
				continue
			}
			parts := strings.SplitN(f, "\t", 3)
			if len(parts) != 3 {
				return nil, fmt.Errorf("git log: malformed numstat %q", f)
			}
			ns := NumStat{Path: parts[2]}
			if parts[0] == "-" || parts[1] == "-" {
				ns.Binary = true
			} else {
				if ns.Added, err = strconv.Atoi(parts[0]); err != nil {
					return nil, fmt.Errorf("git log: numstat %q: %v", f, err)
				}
				if ns.Deleted, err = strconv.Atoi(parts[1]); err != nil {
					return nil, fmt.Errorf("git log: numstat %q: %v", f, err)
				}
			}
			c.Files = append(c.Files, ns)
		}
		commits = append(commits, c)
	}
	return commits, nil
}

// TreeFiles returns the content of every regular file (mode 100644 or
// 100755) in rev's tree for which keep returns true (every one when keep is
// nil), keyed by repo-relative path. Symlinks and submodules are left out.
// Paths are filtered before any blob is read, so skipped files cost nothing.
func TreeFiles(root, rev string, keep func(path string) bool) (map[string][]byte, error) {
	out, err := run(root, nil, "ls-tree", "-r", "-z", "--full-tree", rev)
	if err != nil {
		return nil, err
	}
	var paths []string
	var req bytes.Buffer
	for _, entry := range splitZ(out) {
		// "<mode> <type> <object>\t<path>"
		meta, path, ok := strings.Cut(entry, "\t")
		f := strings.Fields(meta)
		if !ok || len(f) != 3 || f[1] != "blob" || (f[0] != "100644" && f[0] != "100755") {
			continue
		}
		if keep != nil && !keep(path) {
			continue
		}
		paths = append(paths, path)
		req.WriteString(f[2] + "\n")
	}
	files := make(map[string][]byte, len(paths))
	if len(paths) == 0 {
		return files, nil
	}
	blobs, err := run(root, req.Bytes(), "cat-file", "--batch")
	if err != nil {
		return nil, err
	}
	r := bufio.NewReader(bytes.NewReader(blobs))
	for _, p := range paths {
		// "<object> <type> <size>\n<content>\n"
		header, err := r.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("git cat-file: reading %s: %v", p, err)
		}
		f := strings.Fields(header)
		if len(f) != 3 {
			return nil, fmt.Errorf("git cat-file: %s: unexpected header %q", p, header)
		}
		size, err := strconv.Atoi(f[2])
		if err != nil {
			return nil, fmt.Errorf("git cat-file: %s: unexpected header %q", p, header)
		}
		data := make([]byte, size+1) // content plus trailing newline
		if _, err := io.ReadFull(r, data); err != nil {
			return nil, fmt.Errorf("git cat-file: reading %s: %v", p, err)
		}
		files[p] = data[:size]
	}
	return files, nil
}
