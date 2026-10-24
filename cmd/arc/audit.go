package main

import (
	"flag"
	"io"
	"time"

	"arc/internal/audit"
	"arc/internal/beads"
)

// runAudit prints the deterministic drift report (SPEC.md, "arc audit").
// Findings are information, not failures: it exits 0 unless arguments,
// the registry or git fail.
func runAudit(out *output, args []string) int {
	fs := flag.NewFlagSet("audit", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	since := fs.String("since", audit.DefaultSince, "history window: <N>d or <N>w")
	if err := fs.Parse(args); err != nil {
		return out.fail(exitUsage, "audit: "+err.Error())
	}
	if fs.NArg() > 0 {
		return out.fail(exitUsage, "audit: unexpected argument "+fs.Arg(0))
	}
	if _, err := audit.ParseSince(*since); err != nil {
		return out.fail(exitUsage, "audit: "+err.Error())
	}
	r, err := openRepo()
	if err != nil {
		return out.fail(exitUsage, err.Error())
	}
	opts := audit.Options{Root: r.root, Reg: r.reg, Files: r.files, Now: time.Now().UTC(), Since: *since}
	if removalBeads(r) {
		c, notice := beads.Open(r.root)
		opts.BeadsNotice = notice
		if c != nil {
			opts.BeadStatus = func(id string) (string, error) {
				b, err := c.Show(id)
				return b.Status, err
			}
		}
	}
	rep, err := audit.Build(opts)
	if err != nil {
		return out.fail(exitUsage, "audit: "+err.Error())
	}
	out.result(rep.Text(), rep)
	return exitOK
}

// removalBeads reports whether any removal names a bead, so beads is only
// consulted when the report needs it.
func removalBeads(r *repo) bool {
	for _, rm := range r.reg.Removals {
		if rm.Bead != "" {
			return true
		}
	}
	return false
}
