package gate

import (
	"strings"
	"testing"

	"github.com/Yalton/arc/internal/registry"
)

const approvalBase = `version: 1
concepts:
  - id: goal
    paths: ["internal/goal/**"]
    rules: [Goals are work nodes., No goals table.]
  - id: api
    paths: ["internal/api/**"]
  - id: store
    paths: ["internal/store/**"]
  - id: idea
    paths: ["internal/idea/**"]
    draft: true
boundaries:
  - id: no-store
    from: ["internal/api/**"]
    deny: ["internal/store/**"]
  - id: gone
    from: ["internal/api/**"]
    deny: ["internal/goal/**"]
flags:
  - {name: USE_X, concept: goal, expires: 2026-11-01}
  - {name: USE_Y, concept: goal, expires: 2026-11-01}
removals:
  - {id: old, paths: ["internal/old/**"], remove_by: 2026-11-15}
`

var approvalFiles = []string{
	"internal/api/a.go", "internal/api/web/w.go", "internal/goal/g.go",
	"internal/idea/i.go", "internal/new/n.go", "internal/store/s.go", "internal/store/sql/q.go",
}

func approve(t *testing.T, head string, approved bool) []Finding {
	t.Helper()
	parse := func(s string) *registry.Registry {
		r, err := registry.Parse([]byte(s))
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	ctx := &Context{Reg: parse(head), BaseReg: parse(approvalBase), Files: approvalFiles, Base: true, Approved: approved}
	fs, err := checkApproval(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return fs
}

func TestApprovalUnchangedRegistryPasses(t *testing.T) {
	if fs := approve(t, approvalBase, false); len(fs) != 0 {
		t.Fatalf("findings for an unchanged registry: %v", fs)
	}
}

func TestApprovalReportsStructuralChanges(t *testing.T) {
	head := `version: 1
concepts:
  - id: goal
    paths: ["internal/goal/**", "internal/new/**"]
    rules: [Goals are work nodes.]
  - id: api
    paths: ["internal/api/**"]
  - id: store
    paths: ["internal/store/*.go"]
  - id: idea
    paths: ["internal/idea/**"]
  - id: billing
    paths: ["internal/billing/**"]
boundaries:
  - id: no-store
    from: ["internal/**"]
    deny: ["internal/store/*.go"]
flags:
  - {name: USE_X, concept: goal, expires: 2027-01-01}
  - {name: USE_Y, concept: goal, expires: 2026-10-30}
removals:
  - {id: old, paths: ["internal/old/**"], remove_by: 2026-12-01}
`
	fs := approve(t, head, false)
	var got []string
	for _, f := range fs {
		if f.Severity != "" || f.Path != registry.Path ||
			!strings.HasSuffix(f.Message, ". A maintainer approves by adding the arch-approved label to the pull request.") {
			t.Errorf("bad finding %+v", f)
		}
		got = append(got, f.Entry+" | "+strings.TrimPrefix(strings.SplitN(f.Message, ". A maintainer", 2)[0], "needs human approval: "))
	}
	want := []string{
		"concepts[goal].paths | concept goal paths widened to own internal/new/n.go",
		"concepts[idea].draft | draft concept idea promoted to a concept",
		"concepts[billing] | concept billing added",
		`concepts[goal].rules | rule removed from concept goal: "No goals table."`,
		"boundaries[no-store].deny | boundary no-store no longer denies internal/store/sql/q.go",
		"boundaries[no-store].from | boundary no-store from widened to internal/goal/g.go (and 4 more file(s))",
		"boundaries[gone] | boundary gone removed",
		"flags[USE_X].expires | flag USE_X expiry extended from 2026-11-01 to 2027-01-01",
		"removals[old].remove_by | removal old remove_by extended from 2026-11-15 to 2026-12-01",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	// Narrowing paths (store) and shortening an expiry (USE_Y) are not listed.
}

func TestApprovalRemovedConceptRules(t *testing.T) {
	head := strings.Replace(approvalBase, "  - id: goal\n    paths: [\"internal/goal/**\"]\n    rules: [Goals are work nodes., No goals table.]\n", "", 1)
	head = strings.Replace(head, "concept: goal", "concept: api", -1)
	var n int
	for _, f := range approve(t, head, false) {
		if f.Entry == "concepts[goal].rules" {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("removing a concept must report its %d rules, got %d", 2, n)
	}
}

func TestApprovalSkippedWhenApproved(t *testing.T) {
	fs := approve(t, approvalBase+"  - {id: newone, paths: [\"x/**\"], remove_by: 2026-12-01}\n", true)
	if len(fs) != 1 || fs[0].Severity != Info || !strings.Contains(fs[0].Message, "ARC_APPROVED=1") {
		t.Fatalf("approved: %v", fs)
	}
}

func TestApprovalOnlyWithBase(t *testing.T) {
	r := setup(t)
	r.git("branch", "base")
	r.write(".arch/registry.yaml", withReasonless+"    reason: Invoices.\n")
	r.write("internal/billing/b.go", "package billing\n")
	r.render()
	for _, mode := range []string{"working", "all"} {
		if rep := r.gate(mode, "approval"); len(rep.Findings) != 0 {
			t.Fatalf("%s mode ran approval:\n%s", mode, findings(rep))
		}
	}
	r.git("add", "-A")
	if rep := r.gate("staged", "approval"); len(rep.Findings) != 0 {
		t.Fatalf("staged mode ran approval:\n%s", findings(rep))
	}
	r.git("commit", "-q", "-m", "billing")
	rep := r.gate("base", "approval")
	if rep.Errors != 1 || !strings.Contains(findings(rep), "[approval] .arch/registry.yaml: needs human approval: concept billing added.") {
		t.Fatalf("--base:\n%s", findings(rep))
	}
}

func TestInfoFindingsAreNotCounted(t *testing.T) {
	f := Finding{Check: "approval", Severity: Info, Path: registry.Path, Message: "skipped"}
	if got := f.String(); got != "[approval] info: .arch/registry.yaml: skipped" {
		t.Fatalf("String = %q", got)
	}
	ctx := &Context{Reg: &registry.Registry{}, BaseReg: &registry.Registry{}, Base: true, Approved: true}
	rep, err := Run(ctx, []string{"approval"})
	if err != nil || rep.Errors != 0 || rep.Warnings != 0 || len(rep.Findings) != 1 {
		t.Fatalf("rep %+v err %v", rep, err)
	}
}
