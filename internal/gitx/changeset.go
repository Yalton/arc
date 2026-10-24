package gitx

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Status is the kind of change made to a file.
type Status int

// File change statuses. Renames are reported as Deleted plus Added.
const (
	Added Status = iota
	Modified
	Deleted
)

func (s Status) String() string {
	switch s {
	case Added:
		return "added"
	case Modified:
		return "modified"
	case Deleted:
		return "deleted"
	}
	return "Status(" + strconv.Itoa(int(s)) + ")"
}

// Line is one added line.
type Line struct {
	Num  int    // 1-based line number in the new version of the file
	Text string // without trailing newline
}

// FileChange describes one changed file.
type FileChange struct {
	Path   string // repo-relative, slash-separated; new path for renames
	Status Status
	Added  []Line // added lines (empty for Deleted and binary files)
}

// ChangeSet is the set of files changed by a diff.
type ChangeSet struct {
	Files []FileChange // sorted by Path
}

func (c *ChangeSet) paths(s Status) []string {
	var out []string
	for _, f := range c.Files {
		if f.Status == s {
			out = append(out, f.Path)
		}
	}
	return out
}

// Added returns the paths with Status Added.
func (c *ChangeSet) Added() []string { return c.paths(Added) }

// Modified returns the paths with Status Modified.
func (c *ChangeSet) Modified() []string { return c.paths(Modified) }

// Deleted returns the paths with Status Deleted.
func (c *ChangeSet) Deleted() []string { return c.paths(Deleted) }

// File returns the change for path, or nil if path is not in the set.
func (c *ChangeSet) File(path string) *FileChange {
	i := sort.Search(len(c.Files), func(i int) bool { return c.Files[i].Path >= path })
	if i < len(c.Files) && c.Files[i].Path == path {
		return &c.Files[i]
	}
	return nil
}

// BaseChangeSet diffs merge-base(base, HEAD) against HEAD (git diff base...HEAD).
func BaseChangeSet(root, base string) (*ChangeSet, error) {
	if base == "" || strings.HasPrefix(base, "-") {
		return nil, fmt.Errorf("invalid base ref %q", base)
	}
	return changeSet(root, base+"...HEAD")
}

// StagedChangeSet diffs the index against HEAD (git diff --cached). In a
// repository with no commits it diffs against the empty tree.
func StagedChangeSet(root string) (*ChangeSet, error) {
	head, err := headOrEmptyTree(root)
	if err != nil {
		return nil, err
	}
	return changeSet(root, "--cached", head)
}

// WorkingChangeSet diffs the working tree against HEAD (staged and unstaged
// changes) and adds untracked, non-ignored files with all their lines: the
// uncommitted work an agent has in progress.
func WorkingChangeSet(root string) (*ChangeSet, error) {
	head, err := headOrEmptyTree(root)
	if err != nil {
		return nil, err
	}
	cs, err := changeSet(root, head)
	if err != nil {
		return nil, err
	}
	out, err := run(root, nil, "ls-files", "-z", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	for _, path := range splitZ(out) {
		fc := FileChange{Path: path, Status: Added}
		p := filepath.Join(root, filepath.FromSlash(path))
		if fi, err := os.Lstat(p); err == nil && fi.Mode().IsRegular() {
			data, err := os.ReadFile(p)
			if err != nil {
				return nil, err
			}
			fc.Added = ContentLines(data)
		}
		cs.Files = append(cs.Files, fc)
	}
	sort.Slice(cs.Files, func(i, j int) bool { return cs.Files[i].Path < cs.Files[j].Path })
	return cs, nil
}

// ContentLines returns every line of a text file as added lines; binary
// content (containing NUL) yields none.
func ContentLines(data []byte) []Line {
	if len(data) == 0 || bytes.IndexByte(data, 0) >= 0 {
		return nil
	}
	text := strings.TrimSuffix(string(data), "\n")
	var lines []Line
	for i, l := range strings.Split(text, "\n") {
		lines = append(lines, Line{Num: i + 1, Text: strings.TrimSuffix(l, "\r")})
	}
	return lines
}

// headOrEmptyTree returns "HEAD", or the empty tree id in a repository with
// no commits yet (computed for this repository's hash algorithm).
func headOrEmptyTree(root string) (string, error) {
	if _, err := run(root, nil, "rev-parse", "--verify", "--quiet", "HEAD^{commit}"); err == nil {
		return "HEAD", nil
	}
	out, err := run(root, []byte{}, "hash-object", "-t", "tree", "--stdin")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

var diffFlags = []string{
	"diff", "--no-renames", "--no-color", "--no-ext-diff", "--no-textconv",
	"--ignore-submodules=none", "--src-prefix=a/", "--dst-prefix=b/",
}

func changeSet(root string, revs ...string) (*ChangeSet, error) {
	nsArgs := append(append(append([]string{}, diffFlags...), "--name-status", "-z"), revs...)
	ns, err := run(root, nil, append(nsArgs, "--")...)
	if err != nil {
		return nil, err
	}
	files, err := parseNameStatus(ns)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return &ChangeSet{}, nil
	}
	pArgs := append(append(append([]string{}, diffFlags...), "-U0"), revs...)
	patch, err := run(root, nil, append(pArgs, "--")...)
	if err != nil {
		return nil, err
	}
	added, err := parsePatch(patch)
	if err != nil {
		return nil, err
	}
	cs := &ChangeSet{Files: make([]FileChange, 0, len(files))}
	for path, st := range files {
		fc := FileChange{Path: path, Status: st}
		if st != Deleted {
			fc.Added = added[path]
		}
		cs.Files = append(cs.Files, fc)
	}
	sort.Slice(cs.Files, func(i, j int) bool { return cs.Files[i].Path < cs.Files[j].Path })
	return cs, nil
}

// parseNameStatus parses `git diff --name-status -z --no-renames` output.
func parseNameStatus(b []byte) (map[string]Status, error) {
	fields := splitZ(b)
	out := make(map[string]Status, len(fields)/2)
	for i := 0; i < len(fields); i += 2 {
		if i+1 >= len(fields) {
			return nil, fmt.Errorf("git diff --name-status: truncated output")
		}
		code, path := fields[i], fields[i+1]
		var st Status
		switch code[0] {
		case 'A':
			st = Added
		case 'D':
			st = Deleted
		case 'M', 'T', 'U':
			st = Modified
		default:
			return nil, fmt.Errorf("git diff --name-status: unexpected status %q for %s", code, path)
		}
		out[path] = st
	}
	return out, nil
}

// parsePatch extracts added lines per new path from a -U0 unified diff.
func parsePatch(b []byte) (map[string][]Line, error) {
	out := make(map[string][]Line)
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 0, 64*1024), 1<<30)
	var (
		path    string // current new path; "" when /dev/null or unknown
		inHunk  bool
		next    int // next new-file line number
		remain  int // new-side lines left in hunk
		oldLeft int // old-side lines left in hunk
	)
	for sc.Scan() {
		line := sc.Text()
		if inHunk && (remain > 0 || oldLeft > 0) {
			switch {
			case strings.HasPrefix(line, "+"):
				if path != "" {
					out[path] = append(out[path], Line{Num: next, Text: line[1:]})
				}
				next++
				remain--
				continue
			case strings.HasPrefix(line, "-"):
				oldLeft--
				continue
			case strings.HasPrefix(line, " "):
				next++
				remain--
				oldLeft--
				continue
			case strings.HasPrefix(line, `\`):
				continue
			}
		}
		if strings.HasPrefix(line, `\`) {
			continue // "\ No newline at end of file"
		}
		switch {
		case strings.HasPrefix(line, "diff --git "):
			path, inHunk = "", false
		case strings.HasPrefix(line, "+++ "):
			p, err := headerPath(line[4:], "b/")
			if err != nil {
				return nil, err
			}
			path, inHunk = p, false
		case strings.HasPrefix(line, "@@ "):
			start, newCount, oldCount, err := parseHunkHeader(line)
			if err != nil {
				return nil, err
			}
			inHunk, next, remain, oldLeft = true, start, newCount, oldCount
			if newCount == 0 {
				next = start + 1
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("reading git diff output: %w", err)
	}
	return out, nil
}

// headerPath decodes a ---/+++ header path, stripping prefix. It returns ""
// for /dev/null.
func headerPath(s, prefix string) (string, error) {
	s = strings.TrimSuffix(s, "\t") // git appends a tab when the name has spaces
	if s == "/dev/null" {
		return "", nil
	}
	if strings.HasPrefix(s, `"`) {
		u, err := strconv.Unquote(s)
		if err != nil {
			u, err = unquoteC(s)
			if err != nil {
				return "", fmt.Errorf("git diff: bad quoted path %s: %w", s, err)
			}
		}
		s = u
	}
	return strings.TrimPrefix(s, prefix), nil
}

// unquoteC decodes git's C-style quoting (octal escapes for raw bytes).
func unquoteC(s string) (string, error) {
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' {
		return "", fmt.Errorf("not quoted")
	}
	s = s[1 : len(s)-1]
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' {
			b.WriteByte(c)
			continue
		}
		i++
		if i >= len(s) {
			return "", fmt.Errorf("trailing backslash")
		}
		switch c = s[i]; c {
		case 'a':
			b.WriteByte('\a')
		case 'b':
			b.WriteByte('\b')
		case 'f':
			b.WriteByte('\f')
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		case 'v':
			b.WriteByte('\v')
		case '0', '1', '2', '3':
			if i+2 >= len(s) {
				return "", fmt.Errorf("short octal escape")
			}
			n, err := strconv.ParseUint(s[i:i+3], 8, 8)
			if err != nil {
				return "", err
			}
			b.WriteByte(byte(n))
			i += 2
		default:
			b.WriteByte(c)
		}
	}
	return b.String(), nil
}

// parseHunkHeader parses "@@ -a[,b] +c[,d] @@ ..." and returns c, d and b.
func parseHunkHeader(line string) (newStart, newCount, oldCount int, err error) {
	fields := strings.Fields(line)
	if len(fields) < 3 || !strings.HasPrefix(fields[1], "-") || !strings.HasPrefix(fields[2], "+") {
		return 0, 0, 0, fmt.Errorf("git diff: bad hunk header %q", line)
	}
	_, oldCount, err = parseRange(fields[1][1:])
	if err != nil {
		return 0, 0, 0, fmt.Errorf("git diff: bad hunk header %q: %w", line, err)
	}
	newStart, newCount, err = parseRange(fields[2][1:])
	if err != nil {
		return 0, 0, 0, fmt.Errorf("git diff: bad hunk header %q: %w", line, err)
	}
	return newStart, newCount, oldCount, nil
}

func parseRange(s string) (start, count int, err error) {
	count = 1
	if a, b, ok := strings.Cut(s, ","); ok {
		s = a
		if count, err = strconv.Atoi(b); err != nil {
			return 0, 0, err
		}
	}
	start, err = strconv.Atoi(s)
	return start, count, err
}
