package gate

import (
	"time"

	"arc/internal/registry"
)

// checkFlags fails flags whose expiry date has passed.
func checkFlags(ctx *Context) ([]Finding, error) {
	var out []Finding
	for _, f := range ExpiredFlags(ctx.Reg, ctx.Now) {
		out = append(out, Finding{
			Path:  registry.Path,
			Entry: "flags[" + f.Name + "]",
			Message: "flag " + f.Name + " expired on " + f.Expires +
				". Finish the rollout: remove the flag and its old code path, then delete the entry (or extend expires with a reason).",
		})
	}
	return out, nil
}

// ExpiredFlags returns the flags past their expiry date, in registry
// order. A flag is live through its whole expires day. Unparseable dates
// are left to the registry check.
func ExpiredFlags(reg *registry.Registry, now time.Time) []registry.Flag {
	today := now.UTC().Format(registry.DateLayout)
	var out []registry.Flag
	for _, f := range reg.Flags {
		if _, err := f.ExpiresAt(); err == nil && f.Expires < today {
			out = append(out, f)
		}
	}
	return out
}
