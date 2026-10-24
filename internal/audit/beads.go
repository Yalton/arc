package audit

import (
	"fmt"
	"sort"
	"strings"

	"arc/internal/beads"
	"arc/internal/registry"
)

// Labels on beads filed by --file-beads (SPEC.md, "Beads integration
// details"). EntryLabelPrefix plus the entry id deduplicates them.
const (
	LabelConsolidation = "arch:consolidation"
	EntryLabelPrefix   = "arc:"
)

// Proposal is one bead --file-beads would create.
type Proposal struct {
	Entry       string   `json:"entry"` // registry entry, e.g. flags[X] or removals[y]
	Label       string   `json:"label"` // arc:<entry-id>
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Labels      []string `json:"labels"`
}

// Proposals returns one bead per expired flag and overdue removal in the
// report, flags first, in registry order.
func Proposals(rep *Report) []Proposal {
	var out []Proposal
	for _, f := range rep.ExpiredFlags {
		concept := orNone(f.Concept)
		var d strings.Builder
		fmt.Fprintf(&d, "Flag %s (concept %s) expired on %s.\n", f.Name, concept, f.Expires)
		if f.Purpose != "" {
			fmt.Fprintf(&d, "Purpose: %s\n", f.Purpose)
		}
		if f.Bead != "" {
			fmt.Fprintf(&d, "Rollout bead: %s\n", f.Bead)
		}
		fmt.Fprintf(&d, "\nWhat to do: finish the rollout. Remove the flag, its old code path and the flags[%s] entry in %s, "+
			"or extend expires with a reason.\n\n", f.Name, registry.Path)
		templateFields(&d, "flag "+f.Name+" and its old code path", f.Concept)
		out = append(out, proposal("flags["+f.Name+"]", f.Name, "Remove expired flag "+f.Name, d.String()))
	}
	for _, rm := range rep.OverdueRemovals {
		what := rm.What
		if what == "" {
			what = rm.ID
		}
		var d strings.Builder
		fmt.Fprintf(&d, "Removal %s (%s) was due %s.\n", rm.ID, what, rm.RemoveBy)
		if len(rm.Paths) > 0 {
			fmt.Fprintf(&d, "Paths: %s\n", strings.Join(rm.Paths, ", "))
		}
		if rm.ReplacedBy != "" {
			fmt.Fprintf(&d, "Replaced by concept: %s\n", rm.ReplacedBy)
		}
		if rm.Bead != "" {
			fmt.Fprintf(&d, "Tracking bead: %s\n", rm.Bead)
		}
		todo := "Delete the removal paths"
		if rm.ReplacedBy != "" {
			todo = "Move remaining users to " + rm.ReplacedBy + ", then delete the removal paths"
		}
		fmt.Fprintf(&d, "\nWhat to do: %s and the removals[%s] entry in %s, or move remove_by with a reason.\n\n",
			todo, rm.ID, registry.Path)
		templateFields(&d, what, rm.ReplacedBy)
		out = append(out, proposal("removals["+rm.ID+"]", rm.ID, "Finish overdue removal "+rm.ID, d.String()))
	}
	return out
}

// templateFields writes the fields arc triage looks for, then the test
// command.
func templateFields(d *strings.Builder, removes, concept string) {
	fmt.Fprintf(d, "Replaces / removes: %s\nConcepts touched: %s\nClosest existing thing: %s\n\n",
		removes, orNone(concept), orNone(concept))
	d.WriteString("Test: arc gate --all\n")
}

func proposal(entry, id, title, desc string) Proposal {
	label := EntryLabelPrefix + id
	return Proposal{Entry: entry, Label: label, Title: title, Description: desc, Labels: []string{LabelConsolidation, label}}
}

// Filer is the part of the beads client --file-beads needs.
type Filer interface {
	ListLabelled(pattern string) ([]beads.Bead, error)
	Create(title, description string, labels []string) (string, error)
}

// Filed is the outcome for one proposal: a created bead, an existing
// bead that already carries its label, or an error.
type Filed struct {
	Entry    string `json:"entry"`
	Label    string `json:"label"`
	Created  string `json:"created,omitempty"`
	Existing string `json:"existing,omitempty"`
	Error    string `json:"error,omitempty"`
}

// FileBeads creates a bead for each proposal unless a bead in any status
// already carries its arc:<entry-id> label: closed beads count too, so
// work someone closed is not filed again. A failed create is recorded and
// the rest continue; only listing existing beads is fatal.
func FileBeads(f Filer, props []Proposal) ([]Filed, error) {
	out := []Filed{}
	if len(props) == 0 {
		return out, nil
	}
	existing, err := f.ListLabelled(EntryLabelPrefix + "*")
	if err != nil {
		return nil, err
	}
	sort.Slice(existing, func(i, j int) bool { return existing[i].ID < existing[j].ID })
	for _, p := range props {
		res := Filed{Entry: p.Entry, Label: p.Label}
		for _, b := range existing {
			if b.HasLabel(p.Label) {
				res.Existing = b.ID
				break
			}
		}
		if res.Existing == "" {
			if id, err := f.Create(p.Title, p.Description, p.Labels); err != nil {
				res.Error = err.Error()
			} else {
				res.Created = id
			}
		}
		out = append(out, res)
	}
	return out, nil
}

// Summary renders one outcome as a line.
func (f Filed) Summary() string {
	switch {
	case f.Error != "":
		return f.Entry + ": error: " + f.Error
	case f.Existing != "":
		return f.Entry + ": skipped, " + f.Existing + " already has " + f.Label
	}
	return f.Entry + ": created " + f.Created
}
