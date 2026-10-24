package gate

import (
	"strings"
	"testing"
)

const withReasonless = baseRegistry + `  - id: billing
    paths: ["internal/billing/**"]
`

func TestReasonsAdoptionSkipped(t *testing.T) {
	r := newRepo(t, withReasonless)
	r.write("internal/goal/goal.go", "package goal\n")
	r.write("internal/billing/b.go", "package billing\n")
	r.git("add", "-A")
	if rep := r.gate("staged", "reasons"); rep.Errors != 0 {
		t.Fatalf("no registry at HEAD: nothing is added:\n%s", findings(rep))
	}
}

func TestReasonsNewConceptNeedsReason(t *testing.T) {
	r := setup(t) // commits baseRegistry with concept goal (no reason)
	r.write(".arch/registry.yaml", withReasonless)
	r.write("internal/billing/b.go", "package billing\n")
	rep := r.gate("working", "reasons")
	got := findings(rep)
	if rep.Errors != 1 || !strings.Contains(got, "concept billing has no reason") || strings.Contains(got, "goal") {
		t.Fatalf("only the new concept must fail:\n%s", got)
	}
	r.write(".arch/registry.yaml", withReasonless+"    reason: Invoices need one owner.\n")
	if rep := r.gate("working", "reasons"); rep.Errors != 0 {
		t.Fatalf("reason given:\n%s", findings(rep))
	}
	if rep := r.gate("all", "reasons"); rep.Errors != 1 || !strings.Contains(findings(rep), "goal") {
		t.Fatalf("--all requires reasons everywhere:\n%s", findings(rep))
	}
}

func TestReasonsDraftPromotion(t *testing.T) {
	r := newRepo(t, withReasonless+"    draft: true\n")
	r.write("internal/goal/goal.go", "package goal\n")
	r.write("internal/billing/b.go", "package billing\n")
	r.render()
	r.commitAll("adopt")
	if rep := r.gate("working", "reasons"); rep.Errors != 0 {
		t.Fatalf("drafts are exempt:\n%s", findings(rep))
	}
	r.write(".arch/registry.yaml", withReasonless) // draft flag removed
	if rep := r.gate("working", "reasons"); rep.Errors != 1 {
		t.Fatalf("promoting a draft needs a reason:\n%s", findings(rep))
	}
}

func TestWorkingModeSeesUntrackedFiles(t *testing.T) {
	r := setup(t)
	r.write("web/app.go", "package web\n") // untracked, never staged
	if rep := r.gate("working", "placement"); rep.Errors != 1 || !strings.Contains(findings(rep), "web/app.go") {
		t.Fatalf("got:\n%s", findings(rep))
	}
}
