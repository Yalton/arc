// Package gate runs arc's deterministic checks against a change set
// (SPEC.md, "arc gate"). Each check lives in its own file.
package gate

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"arc/internal/gitx"
	"arc/internal/registry"
)

type Severity string

const (
	Error   Severity = "error"
	Warning Severity = "warning"
)

// Finding is one problem a check reports. Entry names the registry entry
// involved, so the agent knows what to edit.
type Finding struct {
	Check    string   `json:"check"`
	Severity Severity `json:"severity"`
	Path     string   `json:"path,omitempty"`
	Line     int      `json:"line,omitempty"`
	Entry    string   `json:"entry,omitempty"`
	Message  string   `json:"message"`
}

func (f Finding) String() string {
	loc := ""
	switch {
	case f.Path != "" && f.Line > 0:
		loc = fmt.Sprintf("%s:%d: ", f.Path, f.Line)
	case f.Path != "":
		loc = f.Path + ": "
	}
	sev := ""
	if f.Severity == Warning {
		sev = "warning: "
	}
	return fmt.Sprintf("[%s] %s%s%s", f.Check, sev, loc, f.Message)
}

// Context is what every check sees.
type Context struct {
	Root    string
	Reg     *registry.Registry
	Files   []string           // every repository file (registry.RepoFiles)
	Changes *gitx.ChangeSet    // the change set; with All, every file as added
	All     bool               // judging the whole tree, not a diff
	Staged  bool               // file contents come from the index
	BaseReg *registry.Registry // registry at the comparison point; nil if none or All
	Now     time.Time
}

// ReadFile returns the content checks should judge for a changed file: the
// staged version with Staged, else the working tree. ok is false for
// anything that is not a regular file (symlinks, submodules, directories)
// or no longer exists.
func (ctx *Context) ReadFile(path string) (data []byte, ok bool, err error) {
	if ctx.Staged {
		return gitx.StagedFile(ctx.Root, path)
	}
	return readRegular(filepath.Join(ctx.Root, filepath.FromSlash(path)))
}

func readRegular(p string) ([]byte, bool, error) {
	fi, err := os.Lstat(p)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil || !fi.Mode().IsRegular() {
		return nil, false, err
	}
	data, err := os.ReadFile(p)
	return data, err == nil, err
}

// Check is one named check.
type Check struct {
	ID  string
	Run func(*Context) ([]Finding, error)
}

// Checks lists the built-in checks in run order.
var Checks = []Check{
	{"registry", checkRegistry},
	{"placement", checkPlacement},
	{"structure", checkStructure},
	{"boundaries", checkBoundaries},
	{"reasons", checkReasons},
	{"flags", checkFlags},
	{"removals", checkRemovals},
	{"size", checkSize},
	{"drafts", checkDrafts},
}

// Report is the result of a gate run.
type Report struct {
	Checks   []string  `json:"checks"`
	Findings []Finding `json:"findings"`
	Errors   int       `json:"errors"`
	Warnings int       `json:"warnings"`
}

// Run executes the checks named in only (all when empty) and collects
// their findings. An unknown check id is an error.
func Run(ctx *Context, only []string) (*Report, error) {
	selected, err := selectChecks(only)
	if err != nil {
		return nil, err
	}
	rep := &Report{Findings: []Finding{}}
	for _, c := range selected {
		fs, err := c.Run(ctx)
		if err != nil {
			return nil, fmt.Errorf("check %s: %w", c.ID, err)
		}
		rep.Checks = append(rep.Checks, c.ID)
		for _, f := range fs {
			f.Check = c.ID
			if f.Severity == "" {
				f.Severity = Error
			}
			if f.Severity == Error {
				rep.Errors++
			} else {
				rep.Warnings++
			}
			rep.Findings = append(rep.Findings, f)
		}
	}
	return rep, nil
}

func selectChecks(only []string) ([]Check, error) {
	if len(only) == 0 {
		return Checks, nil
	}
	want := map[string]bool{}
	for _, id := range only {
		found := false
		for _, c := range Checks {
			found = found || c.ID == id
		}
		if !found {
			return nil, fmt.Errorf("unknown check %q (known: %s)", id, strings.Join(CheckIDs(), ", "))
		}
		want[id] = true
	}
	var out []Check
	for _, c := range Checks {
		if want[c.ID] {
			out = append(out, c)
		}
	}
	return out, nil
}

// CheckIDs returns the ids of all built-in checks.
func CheckIDs() []string {
	ids := make([]string, len(Checks))
	for i, c := range Checks {
		ids[i] = c.ID
	}
	return ids
}

// WholeTree builds a change set that presents every file as added with all
// its lines, so checks judge the whole tree uniformly (gate --all).
// Binary files get no lines.
func WholeTree(root string, files []string) (*gitx.ChangeSet, error) {
	cs := &gitx.ChangeSet{}
	for _, f := range files {
		data, ok, err := readRegular(filepath.Join(root, filepath.FromSlash(f)))
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		fc := gitx.FileChange{Path: f, Status: gitx.Added, Added: gitx.ContentLines(data)}
		cs.Files = append(cs.Files, fc)
	}
	sort.Slice(cs.Files, func(i, j int) bool { return cs.Files[i].Path < cs.Files[j].Path })
	return cs, nil
}
