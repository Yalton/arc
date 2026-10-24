package gate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const datedRegistry = `version: 1
system: {name: demo}
ignore: ["**/*_test.go"]
concepts:
  - id: sched
    paths: ["internal/sched/**"]
flags:
  - {name: OLD_FLAG, concept: sched, expires: 2026-10-23}
  - {name: TODAY_FLAG, concept: sched, expires: 2026-10-24}
  - {name: NEW_FLAG, concept: sched, expires: 2027-01-01}
removals:
  - {id: overdue, what: legacy queue, paths: ["internal/sched/queue.go"], replaced_by: sched, remove_by: 2026-10-01}
  - {id: pending, paths: ["internal/sched/queue.go"], remove_by: 2026-12-01}
  - {id: done, paths: ["internal/gone/**"], remove_by: 2027-01-01}
limits: {file_lines: 10, function_lines: 5}
`

func TestFlags(t *testing.T) {
	r := newRepo(t, datedRegistry)
	r.write("internal/sched/queue.go", "package sched\n")
	got := findings(r.gate("all", "flags"))
	if !strings.Contains(got, "OLD_FLAG expired on 2026-10-23") ||
		strings.Contains(got, "TODAY_FLAG") || strings.Contains(got, "NEW_FLAG") {
		t.Fatalf("got:\n%s", got)
	}
}

func TestRemovals(t *testing.T) {
	r := newRepo(t, datedRegistry)
	r.write("internal/sched/queue.go", "package sched\n")
	rep := r.gate("all", "removals")
	got := findings(rep)
	if rep.Errors != 2 ||
		!strings.Contains(got, "removal overdue (legacy queue) was due 2026-10-01. Move remaining users to sched") ||
		!strings.Contains(got, "removal done is done") || !strings.Contains(got, "Delete the entry") {
		t.Fatalf("errors %d:\n%s", rep.Errors, got)
	}
}

func goFile(funcLines int) string {
	var b strings.Builder
	b.WriteString("package sched\n\nfunc Long() {\n")
	for i := 0; i < funcLines-2; i++ {
		fmt.Fprintf(&b, "\t_ = %d\n", i)
	}
	b.WriteString("}\n")
	return b.String()
}

func TestSizeLongFile(t *testing.T) {
	r := newRepo(t, datedRegistry)
	r.write("internal/sched/big.txt", strings.Repeat("x\n", 11))
	r.write("internal/sched/ok.txt", strings.Repeat("x\n", 10))
	r.write("internal/sched/big_test.go", strings.Repeat("//\n", 50))
	r.git("add", "-A")
	rep := r.gate("staged", "size")
	if rep.Errors != 1 || !strings.Contains(findings(rep), "internal/sched/big.txt: 11 lines, over the limit of 10") {
		t.Fatalf("got:\n%s", findings(rep))
	}
}

func TestSizeLongGoFunctionTouchedVsUntouched(t *testing.T) {
	r := newRepo(t, datedRegistry)
	r.write("internal/sched/f.go", goFile(6)) // 6-line func, limit 5
	r.commitAll("init")

	// Untouched: add a short function after it.
	r.write("internal/sched/f.go", goFile(6)+"\nfunc Short() {}\n")
	r.git("add", "-A")
	if rep := r.gate("staged", "size"); rep.Errors != 0 {
		t.Fatalf("untouched long func must pass:\n%s", findings(rep))
	}
	if rep := r.gate("all", "size"); !strings.Contains(findings(rep), "func Long is 6 lines") {
		t.Fatalf("--all must flag it:\n%s", findings(rep))
	}

	// Touched: edit a line inside Long.
	r.write("internal/sched/f.go", strings.Replace(goFile(6), "_ = 1", "_ = 100", 1))
	r.git("add", "-A")
	rep := r.gate("staged", "size")
	if rep.Errors != 1 || !strings.Contains(findings(rep), "internal/sched/f.go:3: func Long is 6 lines, over the limit of 5") {
		t.Fatalf("touched long func must fail:\n%s", findings(rep))
	}
}

func TestSizeNoLimits(t *testing.T) {
	r := newRepo(t, strings.Replace(datedRegistry, "limits: {file_lines: 10, function_lines: 5}", "", 1))
	r.write("internal/sched/f.go", goFile(100))
	if rep := r.gate("all", "size"); rep.Errors != 0 {
		t.Fatalf("no limits means no check:\n%s", findings(rep))
	}
}

func TestSizeStagedUsesIndexAndSkipsSymlinks(t *testing.T) {
	r := newRepo(t, datedRegistry)
	r.write("internal/sched/a.txt", strings.Repeat("x\n", 11)) // staged: too long
	r.git("add", "-A")
	r.write("internal/sched/a.txt", "x\n") // unstaged fix must not hide it
	rep := r.gate("staged", "size")
	if rep.Errors != 1 {
		t.Fatalf("staged content must be judged:\n%s", findings(rep))
	}
	r.write("internal/sched/a.txt", "x\n")
	r.git("add", "-A")
	r.git("commit", "-q", "-m", "x")
	if err := os.Symlink("../sched", filepath.Join(r.root, "internal/sched/link")); err != nil {
		t.Skip(err)
	}
	r.git("add", "-A")
	if rep := r.gate("staged", "size"); rep.Errors != 0 {
		t.Fatalf("symlink must be skipped:\n%s", findings(rep))
	}
	if rep := r.gate("all", "size"); rep.Errors != 0 {
		t.Fatalf("--all must skip symlink:\n%s", findings(rep))
	}
}
