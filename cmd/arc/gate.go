package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"arc/internal/gate"
	"arc/internal/gitx"
	"arc/internal/registry"
)

// stringList is a repeatable, comma-separable flag.
type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error {
	for _, x := range strings.Split(v, ",") {
		if x = strings.TrimSpace(x); x != "" {
			*s = append(*s, x)
		}
	}
	return nil
}

func runGate(out *output, args []string) int {
	fs := flag.NewFlagSet("gate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	base := fs.String("base", "", "judge the diff from merge-base(ref, HEAD) to HEAD")
	staged := fs.Bool("staged", false, "judge the staged changes")
	all := fs.Bool("all", false, "judge the whole tree")
	var only stringList
	fs.Var(&only, "only", "run only these checks (repeatable)")
	if err := fs.Parse(args); err != nil {
		return out.fail(exitUsage, "gate: "+err.Error())
	}
	modes := 0
	for _, on := range []bool{*base != "", *staged, *all} {
		if on {
			modes++
		}
	}
	if modes > 1 || fs.NArg() > 0 {
		return out.fail(exitUsage, "gate: use at most one of --base <ref>, --staged, --all")
	}

	r, err := openRepo()
	if err != nil {
		return out.fail(exitUsage, err.Error())
	}
	ctx := &gate.Context{Root: r.root, Reg: r.reg, Files: r.files, All: *all, Staged: *staged, Now: time.Now().UTC()}
	baseRev := "HEAD" // where the registry is compared from (reasons check)
	switch {
	case *base != "":
		ctx.Changes, err = gitx.BaseChangeSet(r.root, *base)
		if err == nil {
			baseRev, err = gitx.MergeBase(r.root, *base)
		}
	case *staged:
		ctx.Changes, err = gitx.StagedChangeSet(r.root)
	case *all:
		ctx.Changes, err = gate.WholeTree(r.root, r.files)
	default:
		ctx.Changes, err = gitx.WorkingChangeSet(r.root)
	}
	if err == nil && !*all {
		ctx.BaseReg, err = baseRegistry(r.root, baseRev)
	}
	if err != nil {
		return out.fail(exitUsage, "gate: "+err.Error())
	}

	rep, err := gate.Run(ctx, only)
	if err != nil {
		return out.fail(exitUsage, "gate: "+err.Error())
	}
	var b strings.Builder
	for _, f := range rep.Findings {
		b.WriteString(f.String() + "\n")
	}
	if rep.Errors == 0 && rep.Warnings == 0 {
		b.WriteString("gate: ok")
	} else {
		fmt.Fprintf(&b, "gate: %d error(s), %d warning(s)", rep.Errors, rep.Warnings)
	}
	out.result(b.String(), rep)
	if rep.Errors > 0 {
		return exitViolations
	}
	return exitOK
}

// baseRegistry loads the registry as of rev, or nil when rev has none
// (the change adopts arc) or the repository has no commits.
func baseRegistry(root, rev string) (*registry.Registry, error) {
	data, ok, err := gitx.FileAt(root, rev, registry.Path)
	if err != nil || !ok {
		return nil, err
	}
	reg, err := registry.Parse(data)
	if err != nil {
		// An unreadable old registry must not block today's change; the
		// reasons check then judges nothing as added.
		fmt.Fprintf(os.Stderr, "arc: warning: registry at %s unreadable (%v); skipping reasons check\n", rev, err)
		return nil, nil
	}
	return reg, nil
}
