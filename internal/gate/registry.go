package gate

import "arc/internal/registry"

// checkRegistry reports validation problems and a stale rendered block.
func checkRegistry(ctx *Context) ([]Finding, error) {
	problems := ctx.Reg.Validate(ctx.Files)
	registry.SortProblems(problems)
	var out []Finding
	for _, p := range problems {
		out = append(out, Finding{Path: registry.Path, Entry: p.Entry, Message: p.String()})
	}
	stale, err := ctx.Reg.Stale(ctx.Root)
	if err != nil {
		return nil, err
	}
	for _, f := range stale {
		out = append(out, Finding{Path: f, Message: "arc block is stale or missing. Run 'arc render'."})
	}
	return out, nil
}
