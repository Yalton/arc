package gate

import "arc/internal/registry"

// checkReasons fails concepts the change adds, or promotes from draft,
// without a reason. Without a registry at the comparison point (the change
// adopts arc) nothing counts as added; with --all every non-draft concept
// needs a reason.
func checkReasons(ctx *Context) ([]Finding, error) {
	if ctx.BaseReg == nil && !ctx.All {
		return nil, nil
	}
	var out []Finding
	for _, c := range ctx.Reg.Concepts {
		if c.Draft || c.Reason != "" {
			continue
		}
		if !ctx.All {
			if old := ctx.BaseReg.Concept(c.ID); old != nil && !old.Draft {
				continue // existed before this change
			}
		}
		out = append(out, Finding{
			Path:  registry.Path,
			Entry: "concepts[" + c.ID + "].reason",
			Message: "concept " + c.ID + " has no reason. Add 'reason:' saying why it exists " +
				"and why no existing concept could own it.",
		})
	}
	return out, nil
}
