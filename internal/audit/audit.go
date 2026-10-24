// Package audit builds arc's deterministic drift report and the architect
// prompt around it (SPEC.md, "arc audit"). arc never runs a model: the
// prompt is printed for whatever agent the caller chooses.
package audit

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"arc/internal/gate"
	"arc/internal/gitx"
	"arc/internal/registry"
)

// DefaultSince is the history window when --since is not given.
const DefaultSince = "7d"

// MaxHotspots is how many hotspot files the report lists.
const MaxHotspots = 10

// Options is everything the report depends on, so it is reproducible:
// the repository state, the registry, the window and the clock.
type Options struct {
	Root  string
	Reg   *registry.Registry
	Files []string // current repository files (registry.RepoFiles)
	Now   time.Time
	Since string // window: "<N>d" or "<N>w"; DefaultSince when empty
	// BeadStatus returns a bead's status; nil when beads is unavailable,
	// in which case BeadsNotice says why.
	BeadStatus  func(id string) (string, error)
	BeadsNotice string
}

// Report is the drift report. Lists are empty, not nil, so --json always
// has every field.
type Report struct {
	Since              string           `json:"since"`
	From               string           `json:"from"` // window start (UTC)
	To                 string           `json:"to"`   // now (UTC)
	Base               string           `json:"base"` // newest commit before From; "" when history starts later
	Concepts           []ConceptLOC     `json:"concepts"`
	History            History          `json:"history"`
	Hotspots           []Hotspot        `json:"hotspots"`
	Unowned            []string         `json:"unowned_files"`
	Drafts             []string         `json:"draft_concepts"`
	NoReason           []string         `json:"concepts_without_reason"` // non-draft only
	MultiStorage       []ConceptStorage `json:"multi_storage_concepts"`
	ExpiredFlags       []FlagItem       `json:"expired_flags"`
	OverdueRemovals    []RemovalItem    `json:"overdue_removals"`
	ClosedBeadRemovals []RemovalItem    `json:"closed_bead_removals"`
	Checks             []CheckResult    `json:"checks"`
	Notices            []string         `json:"notices"`
}

// ConceptLOC is a concept's non-test line count at the window start and
// now. Both use today's ownership globs, so growth is comparable.
type ConceptLOC struct {
	ID     string `json:"id"`
	Before int    `json:"before"`
	Now    int    `json:"now"`
	Delta  int    `json:"delta"`
}

// History summarises the non-merge commits in the window.
type History struct {
	Commits int `json:"commits"`
	Added   int `json:"lines_added"`
	Deleted int `json:"lines_deleted"`
	Fix     int `json:"fix_commits"`
	Feat    int `json:"feat_commits"`
	// FixFeatRatio is Fix/Feat; nil without feat commits.
	FixFeatRatio *float64 `json:"fix_feat_ratio"`
}

// Hotspot is a file touched by fix commits in the window.
type Hotspot struct {
	Path       string `json:"path"`
	FixCommits int    `json:"fix_commits"`
}

type ConceptStorage struct {
	ID      string   `json:"id"`
	Storage []string `json:"storage"`
}

type FlagItem struct {
	Name    string `json:"name"`
	Concept string `json:"concept,omitempty"`
	Purpose string `json:"purpose,omitempty"`
	Expires string `json:"expires"`
	Bead    string `json:"bead,omitempty"`
}

type RemovalItem struct {
	ID         string   `json:"id"`
	What       string   `json:"what,omitempty"`
	Paths      []string `json:"paths,omitempty"`
	ReplacedBy string   `json:"replaced_by,omitempty"`
	RemoveBy   string   `json:"remove_by,omitempty"`
	Bead       string   `json:"bead,omitempty"`
}

// CheckResult is one external check's findings split by its baseline.
// New lists the findings beyond the baseline; Error is set instead when
// the command could not run.
type CheckResult struct {
	ID        string   `json:"id"`
	Findings  int      `json:"findings"`
	Baselined int      `json:"baselined"`
	New       []string `json:"new"`
	Error     string   `json:"error,omitempty"`
}

var sinceRE = regexp.MustCompile(`^([1-9][0-9]{0,4})([dw])$`)

// ParseSince returns the number of days in a "<N>d" or "<N>w" window.
func ParseSince(s string) (int, error) {
	m := sinceRE.FindStringSubmatch(s)
	if m == nil {
		return 0, fmt.Errorf("invalid --since %q; use <N>d or <N>w, e.g. 7d or 2w", s)
	}
	n, _ := strconv.Atoi(m[1])
	if m[2] == "w" {
		n *= 7
	}
	return n, nil
}

// Build computes the report. Errors are configuration or git failures;
// findings never are.
func Build(o Options) (*Report, error) {
	if o.Since == "" {
		o.Since = DefaultSince
	}
	days, err := ParseSince(o.Since)
	if err != nil {
		return nil, err
	}
	now := o.Now.UTC()
	from := now.AddDate(0, 0, -days)
	rep := &Report{
		Since: o.Since, From: from.Format(time.RFC3339), To: now.Format(time.RFC3339),
		Hotspots: []Hotspot{}, Unowned: []string{}, Drafts: []string{}, NoReason: []string{}, MultiStorage: []ConceptStorage{},
		ExpiredFlags: []FlagItem{}, OverdueRemovals: []RemovalItem{}, ClosedBeadRemovals: []RemovalItem{},
		Checks: []CheckResult{}, Notices: []string{},
	}
	if rep.Base, err = gitx.CommitBefore(o.Root, from); err != nil {
		return nil, err
	}
	if err := rep.addLOC(o); err != nil {
		return nil, err
	}
	commits, err := gitx.Log(o.Root, rep.Base)
	if err != nil {
		return nil, err
	}
	rep.addHistory(commits)
	rep.addRegistry(o, now)
	rep.addRemovalBeads(o)
	rep.addChecks(o)
	return rep, nil
}

// addLOC counts non-test lines per concept in the working tree and at Base.
func (r *Report) addLOC(o Options) error {
	nowLOC := map[string]int{}
	for _, f := range o.Files {
		c := o.Reg.Owner(f)
		if c == nil || IsTest(f) {
			continue
		}
		data, ok, err := readRegular(o.Root, f)
		if err != nil {
			return err
		}
		if ok {
			nowLOC[c.ID] += len(gitx.ContentLines(data))
		}
	}
	oldLOC := map[string]int{}
	if r.Base != "" {
		old, err := gitx.TreeFiles(o.Root, r.Base)
		if err != nil {
			return err
		}
		for f, data := range old {
			if c := o.Reg.Owner(f); c != nil && !IsTest(f) {
				oldLOC[c.ID] += len(gitx.ContentLines(data))
			}
		}
	}
	r.Concepts = []ConceptLOC{}
	for _, c := range o.Reg.Concepts {
		r.Concepts = append(r.Concepts, ConceptLOC{ID: c.ID, Before: oldLOC[c.ID], Now: nowLOC[c.ID], Delta: nowLOC[c.ID] - oldLOC[c.ID]})
	}
	return nil
}

// commitType matches a conventional-commit prefix: "fix:", "feat(x):", "fix!:".
var commitType = regexp.MustCompile(`^(\w+)(?:\([^)]*\))?!?:`)

// addHistory totals line counts, commit types and fix hotspots.
func (r *Report) addHistory(commits []gitx.Commit) {
	fixes := map[string]int{}
	for _, c := range commits {
		r.History.Commits++
		typ := ""
		if m := commitType.FindStringSubmatch(c.Subject); m != nil {
			typ = strings.ToLower(m[1])
		}
		switch typ {
		case "fix":
			r.History.Fix++
		case "feat":
			r.History.Feat++
		}
		for _, f := range c.Files {
			r.History.Added += f.Added
			r.History.Deleted += f.Deleted
			if typ == "fix" {
				fixes[f.Path]++
			}
		}
	}
	if r.History.Feat > 0 {
		ratio := float64(r.History.Fix) / float64(r.History.Feat)
		r.History.FixFeatRatio = &ratio
	}
	for p, n := range fixes {
		r.Hotspots = append(r.Hotspots, Hotspot{Path: p, FixCommits: n})
	}
	sort.Slice(r.Hotspots, func(i, j int) bool {
		a, b := r.Hotspots[i], r.Hotspots[j]
		if a.FixCommits != b.FixCommits {
			return a.FixCommits > b.FixCommits
		}
		return a.Path < b.Path
	})
	if len(r.Hotspots) > MaxHotspots {
		r.Hotspots = r.Hotspots[:MaxHotspots]
	}
}

// addRegistry adds the findings that come from the registry alone.
func (r *Report) addRegistry(o Options, now time.Time) {
	for _, f := range o.Files {
		if !o.Reg.IsIgnored(f) && o.Reg.Owner(f) == nil {
			r.Unowned = append(r.Unowned, f)
		}
	}
	for _, c := range o.Reg.Concepts {
		if c.Draft {
			r.Drafts = append(r.Drafts, c.ID)
		} else if c.Reason == "" {
			r.NoReason = append(r.NoReason, c.ID)
		}
		if len(c.Storage) > 1 {
			r.MultiStorage = append(r.MultiStorage, ConceptStorage{ID: c.ID, Storage: c.Storage})
		}
	}
	for _, f := range gate.ExpiredFlags(o.Reg, now) {
		r.ExpiredFlags = append(r.ExpiredFlags, FlagItem{Name: f.Name, Concept: f.Concept, Purpose: f.Purpose, Expires: f.Expires, Bead: f.Bead})
	}
	for _, rm := range gate.OverdueRemovals(o.Reg, o.Files, now) {
		r.OverdueRemovals = append(r.OverdueRemovals, removalItem(rm))
	}
}

// addRemovalBeads lists removals whose bead is closed while their paths
// remain. Without beads, or when a bead cannot be read, it adds a notice.
func (r *Report) addRemovalBeads(o Options) {
	for _, rm := range o.Reg.Removals {
		if rm.Bead == "" || len(rm.Paths) == 0 || gate.RemovalDone(rm, o.Files) {
			continue
		}
		if o.BeadStatus == nil {
			notice := o.BeadsNotice
			if notice == "" {
				notice = "beads: unavailable"
			}
			r.Notices = append(r.Notices, notice+"; removal bead status not checked")
			return
		}
		status, err := o.BeadStatus(rm.Bead)
		if err != nil {
			r.Notices = append(r.Notices, fmt.Sprintf("beads: removal %s: %v", rm.ID, err))
			continue
		}
		if status == "closed" {
			r.ClosedBeadRemovals = append(r.ClosedBeadRemovals, removalItem(rm))
		}
	}
}

// addChecks runs every external check on the whole tree, counting
// baselined findings. Findings and failures are reported, never fatal.
func (r *Report) addChecks(o Options) {
	for _, k := range o.Reg.Checks {
		res := CheckResult{ID: gate.ExternalPrefix + k.ID, New: []string{}}
		lines, err := gate.RunExternal(o.Root, k)
		if err == nil {
			var fresh []string
			fresh, res.Baselined, err = gate.SplitBaselined(o.Root, k, lines)
			res.Findings = len(lines)
			res.New = append(res.New, fresh...)
		}
		if err != nil {
			res = CheckResult{ID: res.ID, New: []string{}, Error: err.Error()}
		}
		r.Checks = append(r.Checks, res)
	}
}

func removalItem(rm registry.Removal) RemovalItem {
	return RemovalItem{ID: rm.ID, What: rm.What, Paths: rm.Paths, ReplacedBy: rm.ReplacedBy, RemoveBy: rm.RemoveBy, Bead: rm.Bead}
}

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
