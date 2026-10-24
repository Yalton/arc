package main

import (
	"fmt"
	"strings"

	"arc/internal/gate"
)

// runBaseline records an external check's current findings as accepted
// (SPEC.md, "arc baseline").
func runBaseline(out *output, args []string) int {
	if len(args) != 1 || strings.HasPrefix(args[0], "-") {
		return out.fail(exitUsage, "baseline: usage: arc baseline <check-id>")
	}
	r, err := openRepo()
	if err != nil {
		return out.fail(exitUsage, err.Error())
	}
	k := gate.ExternalCheck(r.reg, args[0])
	if k == nil {
		var ids []string
		for _, c := range r.reg.Checks {
			ids = append(ids, c.ID)
		}
		known := "none; add one under checks in .arch/registry.yaml"
		if len(ids) > 0 {
			known = strings.Join(ids, ", ")
		}
		return out.fail(exitUsage, fmt.Sprintf("baseline: unknown check %q (known: %s)", args[0], known))
	}
	if !k.Baseline {
		return out.fail(exitUsage, fmt.Sprintf("baseline: checks[%s] has baseline: false, so a baseline would be ignored. Set baseline: true first.", k.ID))
	}
	res, err := gate.RecordBaseline(r.root, *k)
	if err != nil {
		return out.fail(exitUsage, "baseline: "+err.Error())
	}
	text := fmt.Sprintf("%s: %d finding(s) accepted in %s (%d added, %d removed)",
		res.Check, res.Findings, res.Path, res.Added, res.Removed)
	if len(res.AddedSample) > 0 {
		// A misconfigured check is accepted too; show what was added.
		text += "\nnewly accepted"
		if res.Added > len(res.AddedSample) {
			text += fmt.Sprintf(" (first %d of %d)", len(res.AddedSample), res.Added)
		}
		text += ":\n  " + strings.Join(res.AddedSample, "\n  ")
	}
	out.result(text, res)
	return exitOK
}
