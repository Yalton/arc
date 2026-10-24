package beads

import (
	"strings"
	"testing"
)

func TestListLabelledIncludesClosed(t *testing.T) {
	c, state := fakeBd(t)
	writeState(t, state, "list.json", `[
	  {"id": "a-1", "status": "closed", "labels": ["arc:old-queue", "arch:consolidation"]},
	  {"id": "a-2", "status": "open", "labels": ["arc:FLAG"]}
	]`)
	bs, err := c.ListLabelled("arc:*")
	if err != nil {
		t.Fatal(err)
	}
	if len(bs) != 2 || bs[0].Status != "closed" || !bs[1].HasLabel("arc:FLAG") {
		t.Fatalf("got %+v", bs)
	}
	if calls := readState(t, state, "calls"); !strings.Contains(calls, "list --all --label-pattern=arc:* --limit=0 --json") {
		t.Fatalf("calls %q", calls)
	}
}
