package hook

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Yalton/arc/internal/brief"
	"github.com/Yalton/arc/internal/gitx"
	"github.com/Yalton/arc/internal/registry"
)

// sessionState is the per-session hook state in
// .arch/cache/hook-<session>.json. Older arc versions wrote a bare JSON
// array of concept ids; it is read as Concepts.
type sessionState struct {
	// Concepts briefed this session.
	Concepts []string `json:"concepts"`
	// New lists the new files already announced this session (a brief's
	// provides repeated, or an unowned report), so a shell command run
	// later does not announce them again.
	New []string `json:"new,omitempty"`
	// Snapshot maps each dirty path, as of the last shell command, to its
	// signature (see shellChanges). nil until the first shell command of
	// the session.
	Snapshot map[string]string `json:"snapshot"`

	path  string // "" without a session id: nothing persists
	dirty bool
}

var unsafeID = regexp.MustCompile(`[^A-Za-z0-9._-]`)

// loadSession reads the state for session. Missing or corrupt state starts
// over; without a session id the state is empty and never saved.
func loadSession(root, session string) *sessionState {
	st := &sessionState{}
	if session == "" {
		return st
	}
	st.path = filepath.Join(root, filepath.FromSlash(cacheDir), "hook-"+unsafeID.ReplaceAllString(session, "_")+".json")
	data, err := os.ReadFile(st.path)
	if err != nil {
		return st
	}
	if json.Unmarshal(data, st) != nil {
		var legacy []string
		if json.Unmarshal(data, &legacy) == nil {
			st.Concepts = legacy
		}
	}
	return st
}

// markConcepts records concepts as briefed and returns the ones not
// briefed before. Without a session id every concept is fresh.
func (st *sessionState) markConcepts(concepts []string) []string {
	if st.path == "" {
		return concepts
	}
	var fresh []string
	for _, c := range concepts {
		if !contains(st.Concepts, c) {
			fresh = append(fresh, c)
			st.Concepts = append(st.Concepts, c)
			st.dirty = true
		}
	}
	return fresh
}

// markNew records new file p as announced and reports whether it was not
// announced before.
func (st *sessionState) markNew(p string) bool {
	if contains(st.New, p) {
		return false
	}
	st.New = append(st.New, p)
	st.dirty = true
	return true
}

func (st *sessionState) save() error {
	if st.path == "" || !st.dirty {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(st.path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(st)
	if err != nil {
		return err
	}
	tmp := st.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, st.path)
}

// firstShellWindow bounds what the first shell command of a session is
// held responsible for: with no snapshot to compare against, a dirty file
// counts as changed by it only when modified this recently. Older dirty
// files predate the session (or at least this hook) and are not reported.
const firstShellWindow = 10 * time.Minute

// maxUnownedListed caps the unowned files one report lists.
const maxUnownedListed = 10

// shellChanges looks at the working tree after a shell command: one
// 'git status' call, compared with the session's last snapshot. A path
// counts as changed when it is dirty and its signature (porcelain code,
// modification time, size) differs from the snapshot, or, on the first
// shell command of the session, when it was modified within
// firstShellWindow. Deleted paths and non-regular files are skipped.
//
// Of the changed paths, new ones (untracked or added to the index) that no
// concept owns are returned as Feedback with the place message, once per
// path per session. Owned ones feed the brief: concepts not briefed this
// session get their brief, and a new file in an already briefed concept
// gets that concept's provides, as in a pre-write hook. Both are returned
// as Context.
func shellChanges(root string, reg *registry.Registry, session string, now time.Time) (Decision, error) {
	entries, err := gitx.WorktreeStatus(root)
	if err != nil {
		return Decision{}, err
	}
	st := loadSession(root, session)
	first := st.Snapshot == nil
	cutoff := time.Now().Add(-firstShellWindow) // file times are wall clock; now dates the brief
	snap := make(map[string]string, len(entries))
	var concepts, created, unowned []string
	for _, e := range entries {
		fi, err := os.Lstat(filepath.Join(root, filepath.FromSlash(e.Path)))
		if err != nil || !fi.Mode().IsRegular() {
			continue
		}
		sig := fmt.Sprintf("%s %d %d", e.Code, fi.ModTime().UnixNano(), fi.Size())
		snap[e.Path] = sig
		if first && fi.ModTime().Before(cutoff) || !first && st.Snapshot[e.Path] == sig {
			continue
		}
		isNew := e.Code == "??" || e.Code[0] == 'A'
		res := brief.Place(reg, e.Path)
		switch res.Status {
		case brief.StatusUnowned:
			if isNew && st.markNew(e.Path) {
				unowned = append(unowned, brief.PlaceMessage(res))
			}
		case brief.StatusOwned:
			concepts = appendUnique(concepts, res.Concept)
			if isNew && st.markNew(e.Path) {
				created = appendUnique(created, res.Concept)
			}
		}
	}
	if st.path != "" {
		st.Snapshot, st.dirty = snap, true
	}
	fresh := st.markConcepts(concepts)
	var again []string
	for _, c := range created {
		if !contains(fresh, c) {
			again = append(again, c)
		}
	}
	var d Decision
	if d.Context, err = briefContext(root, reg, fresh, again, "the code your last command changed", now); err != nil {
		return Decision{}, err
	}
	if len(unowned) > 0 {
		lines := unowned
		if len(lines) > maxUnownedListed {
			lines = append(lines[:maxUnownedListed:maxUnownedListed],
				fmt.Sprintf("... and %d more ('arc gate' lists them all)", len(unowned)-maxUnownedListed))
		}
		d.Feedback = "arc: your last command created file(s) that no registry concept owns, so 'arc gate' will fail on them. " +
			"Move each into an existing concept's paths, or register an owner in " + registry.Path + " with a reason:\n- " +
			strings.Join(lines, "\n- ")
	}
	return d, st.save()
}
