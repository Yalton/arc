package gate

import "arc/internal/registry"

// checkFlags fails flags whose expiry date has passed. A flag is live
// through its whole expires day. Unparseable dates are left to the
// registry check.
func checkFlags(ctx *Context) ([]Finding, error) {
	today := ctx.Now.UTC().Format(registry.DateLayout)
	var out []Finding
	for _, f := range ctx.Reg.Flags {
		if _, err := f.ExpiresAt(); err != nil || f.Expires >= today {
			continue
		}
		out = append(out, Finding{
			Path:  registry.Path,
			Entry: "flags[" + f.Name + "]",
			Message: "flag " + f.Name + " expired on " + f.Expires +
				". Finish the rollout: remove the flag and its old code path, then delete the entry (or extend expires with a reason).",
		})
	}
	return out, nil
}
