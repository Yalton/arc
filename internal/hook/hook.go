// Package hook holds the harness-agnostic decisions behind 'arc hook'
// (SPEC.md, "arc hook"). Per-harness files translate payloads to an Event
// and a Decision back to the harness's response format.
package hook

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"arc/internal/beads"
	"arc/internal/brief"
	"arc/internal/gitx"
	"arc/internal/registry"
)

type Kind int

const (
	Other     Kind = iota // nothing for arc to do
	PreWrite              // a tool is about to create or edit files
	PostShell             // a shell command finished
)

// Event is a harness payload reduced to what arc needs.
type Event struct {
	Kind      Kind
	SessionID string
	Cwd       string   // the agent's working directory
	Paths     []string // PreWrite targets, absolute or relative to Cwd
	Command   string   // PostShell command line
	Output    string   // PostShell stdout, when the harness provides it
}

// Decision is arc's answer. The zero value allows silently.
type Decision struct {
	Deny    bool
	Reason  string // why, when denying
	Context string // added context for the agent, when allowing
	Warning string // arc-internal problem; the action is still allowed
}

// cacheDir holds per-session hook state (gitignored by arc init).
const cacheDir = ".arch/cache"

// Decide answers ev. It never fails: any internal error yields an allow
// with a warning, so a broken arc install never blocks an agent.
func Decide(ev Event, now time.Time) (d Decision) {
	defer func() {
		if r := recover(); r != nil {
			d = Decision{Warning: fmt.Sprintf("arc hook: internal error: %v", r)}
		}
	}()
	var err error
	switch ev.Kind {
	case PreWrite:
		d, err = preWrite(ev, now)
	case PostShell:
		d, err = postShell(ev, now)
	}
	if err != nil {
		return Decision{Warning: "arc hook: " + err.Error()}
	}
	return d
}

func preWrite(ev Event, now time.Time) (Decision, error) {
	if len(ev.Paths) == 0 {
		return Decision{}, nil
	}
	abs := make([]string, len(ev.Paths))
	for i, p := range ev.Paths {
		if !filepath.IsAbs(p) {
			p = filepath.Join(ev.Cwd, p)
		}
		abs[i] = filepath.Clean(p)
	}
	root, err := findRoot(filepath.Dir(abs[0]))
	if err != nil {
		return Decision{}, nil // not in a repository arc manages
	}
	reg, err := registry.Load(root)
	if errors.Is(err, registry.ErrNotFound) {
		return Decision{}, nil
	}
	if err != nil {
		return Decision{}, err
	}

	var concepts []string
	for _, p := range abs {
		rel, ok := gitx.Rel(root, p)
		if !ok {
			continue // outside the repository
		}
		res := brief.Place(reg, rel)
		switch res.Status {
		case brief.StatusUnowned:
			if _, err := os.Stat(p); errors.Is(err, os.ErrNotExist) {
				return Decision{Deny: true, Reason: "arc: " + brief.PlaceMessage(res) +
					" Run 'arc place " + rel + "' to check a path."}, nil
			}
		case brief.StatusOwned:
			concepts = appendUnique(concepts, res.Concept)
		}
	}
	fresh, err := markSeen(root, ev.SessionID, concepts)
	if err != nil || len(fresh) == 0 {
		return Decision{}, err
	}
	b, err := brief.ForConcepts(reg, fresh, now)
	if err != nil {
		return Decision{}, err
	}
	return Decision{Context: "arc architecture brief for the code you are editing:\n" + b.Text()}, nil
}

// bdCreate matches a 'bd create' invocation in a shell command line.
var bdCreate = regexp.MustCompile(`(^|[\s;&|(])bd\s+create\b`)

// createdIssue finds bead ids in 'bd create' output
// ("✓ Created issue: arc-1 — Title").
var createdIssue = regexp.MustCompile(`Created issue:\s*([A-Za-z0-9][A-Za-z0-9._-]*)`)

// newBeadWindow bounds the fallback when the output names no bead: open,
// untriaged beads created this recently are taken to be the new ones.
const newBeadWindow = 2 * time.Minute

// postShell triages beads created by a 'bd create' shell command and
// returns the outcome as context (SPEC.md, "arc hook").
func postShell(ev Event, now time.Time) (Decision, error) {
	if !bdCreate.MatchString(ev.Command) {
		return Decision{}, nil
	}
	root, err := findRoot(ev.Cwd)
	if err != nil {
		return Decision{}, nil
	}
	reg, err := registry.Load(root)
	if errors.Is(err, registry.ErrNotFound) {
		return Decision{}, nil
	}
	if err != nil {
		return Decision{}, err
	}
	c, notice := beads.Open(root)
	if c == nil {
		return Decision{Warning: "arc hook: " + notice}, nil
	}
	var ids []string
	for _, m := range createdIssue.FindAllStringSubmatch(ev.Output, -1) {
		ids = appendUnique(ids, m[1])
	}
	results, err := beads.Run(c, reg, ids, now.Add(-newBeadWindow), now, false)
	if err != nil {
		return Decision{}, err
	}
	var d Decision
	var lines, warnings []string
	for _, r := range results {
		if r.Error != "" {
			warnings = append(warnings, "arc hook: triage "+r.ID+": "+r.Error)
			continue
		}
		lines = append(lines, "- "+r.Summary())
	}
	d.Warning = strings.Join(warnings, "\n")
	if len(lines) > 0 {
		d.Context = "arc triage of the new bead(s):\n" + strings.Join(lines, "\n") +
			"\nFor arch:review, fix the bead (template fields, existing concepts) or ask for review; the concept brief is in the bead notes."
	}
	return d, nil
}

// findRoot returns the git top-level containing dir, walking up to the
// nearest existing directory first (the target directory may be new).
func findRoot(dir string) (string, error) {
	for {
		if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
			return gitx.Root(dir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("no existing directory")
		}
		dir = parent
	}
}

var unsafeID = regexp.MustCompile(`[^A-Za-z0-9._-]`)

// markSeen records concepts as briefed in this session and returns the
// ones not briefed before. Without a session id every brief is fresh.
func markSeen(root, session string, concepts []string) ([]string, error) {
	if len(concepts) == 0 || session == "" {
		return concepts, nil
	}
	dir := filepath.Join(root, filepath.FromSlash(cacheDir))
	path := filepath.Join(dir, "hook-"+unsafeID.ReplaceAllString(session, "_")+".json")
	var seen []string
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &seen) // corrupt state: start over
	}
	var fresh []string
	for _, c := range concepts {
		if !contains(seen, c) {
			fresh = append(fresh, c)
			seen = append(seen, c)
		}
	}
	if len(fresh) == 0 {
		return nil, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fresh, err
	}
	data, _ := json.Marshal(seen)
	return fresh, os.WriteFile(path, data, 0o644)
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

func appendUnique(xs []string, x string) []string {
	if contains(xs, x) {
		return xs
	}
	return append(xs, x)
}
