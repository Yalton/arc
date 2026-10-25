package audit

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Yalton/arc/internal/beads"
)

type fakeFiler struct {
	existing []beads.Bead
	listErr  error
	created  []Proposal
	failOn   string
}

func (f *fakeFiler) ListLabelled(pattern string) ([]beads.Bead, error) {
	if pattern != "arc:*" {
		return nil, errors.New("unexpected pattern " + pattern)
	}
	return f.existing, f.listErr
}

func (f *fakeFiler) Create(title, desc string, labels []string) (string, error) {
	if title == f.failOn {
		return "", errors.New("bd create: boom")
	}
	f.created = append(f.created, Proposal{Title: title, Description: desc, Labels: labels})
	return "new-" + labels[1], nil
}

func TestProposals(t *testing.T) {
	ps := Proposals(sampleReport())
	if len(ps) != 2 {
		t.Fatalf("got %+v", ps)
	}
	f, r := ps[0], ps[1]
	if f.Entry != "flags[OLD_FLAG]" || f.Title != "Remove expired flag OLD_FLAG" ||
		!reflect.DeepEqual(f.Labels, []string{"arch:consolidation", "arc:OLD_FLAG"}) {
		t.Errorf("flag proposal %+v", f)
	}
	if r.Entry != "removals[old-a]" || r.Label != "arc:old-a" || r.Title != "Finish overdue removal old-a" {
		t.Errorf("removal proposal %+v", r)
	}
	for _, want := range []string{"Flag OLD_FLAG (concept alpha) expired on 2026-01-10.", "flags[OLD_FLAG] entry",
		"Concepts touched: alpha", "Test: arc gate --all"} {
		if !strings.Contains(f.Description, want) {
			t.Errorf("flag description missing %q:\n%s", want, f.Description)
		}
	}
	for _, want := range []string{"Removal old-a (legacy a) was due 2026-01-14.", "Move remaining users to alpha",
		"removals[old-a] entry", "Replaces / removes: legacy a", "Test: arc gate --all"} {
		if !strings.Contains(r.Description, want) {
			t.Errorf("removal description missing %q:\n%s", want, r.Description)
		}
	}
}

func TestFileBeadsDeduplicates(t *testing.T) {
	f := &fakeFiler{existing: []beads.Bead{
		{ID: "b-2", Status: "open", Labels: []string{"arc:old-a"}},
		{ID: "b-1", Status: "closed", Labels: []string{"arch:consolidation", "arc:old-a"}},
		{ID: "b-3", Status: "open", Labels: []string{"arc:OLD_FLAG_2"}},
	}}
	got, err := FileBeads(f, Proposals(sampleReport()))
	if err != nil {
		t.Fatal(err)
	}
	want := []Filed{
		{Entry: "flags[OLD_FLAG]", Label: "arc:OLD_FLAG", Created: "new-arc:OLD_FLAG"},
		{Entry: "removals[old-a]", Label: "arc:old-a", Existing: "b-1"},
	}
	if !reflect.DeepEqual(got, want) || len(f.created) != 1 {
		t.Fatalf("got %+v created %d", got, len(f.created))
	}
	if got[1].Summary() != "removals[old-a]: skipped, b-1 already has arc:old-a" || got[0].Summary() != "flags[OLD_FLAG]: created new-arc:OLD_FLAG" {
		t.Errorf("summaries %q / %q", got[0].Summary(), got[1].Summary())
	}
}

func TestFileBeadsErrors(t *testing.T) {
	if _, err := FileBeads(&fakeFiler{listErr: errors.New("bd list: down")}, Proposals(sampleReport())); err == nil {
		t.Error("list error ignored")
	}
	f := &fakeFiler{failOn: "Remove expired flag OLD_FLAG"}
	got, err := FileBeads(f, Proposals(sampleReport()))
	if err != nil || got[0].Error == "" || got[1].Created == "" {
		t.Errorf("got %+v, %v", got, err)
	}
	if got, err := FileBeads(&fakeFiler{listErr: errors.New("unused")}, nil); err != nil || len(got) != 0 {
		t.Errorf("no proposals: %+v, %v", got, err)
	}
}
