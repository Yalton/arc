package gate

import (
	"strings"

	"arc/internal/registry"
)

// checkRemovals fails removals past their deadline, and removals whose
// paths are all gone but whose entry remains.
func checkRemovals(ctx *Context) ([]Finding, error) {
	today := ctx.Now.UTC().Format(registry.DateLayout)
	var out []Finding
	for _, rm := range ctx.Reg.Removals {
		entry := "removals[" + rm.ID + "]"
		what := rm.What
		if what == "" {
			what = rm.ID
		}
		if len(rm.Paths) > 0 && !anyFileMatches(ctx.Files, rm.Paths) {
			out = append(out, Finding{Path: registry.Path, Entry: entry,
				Message: "removal " + rm.ID + " is done (no files match " + strings.Join(rm.Paths, ", ") +
					"). Delete the entry."})
			continue
		}
		if _, err := rm.RemoveByAt(); err != nil || rm.RemoveBy >= today {
			continue
		}
		msg := "removal " + rm.ID + " (" + what + ") was due " + rm.RemoveBy + "."
		if rm.ReplacedBy != "" {
			msg += " Move remaining users to " + rm.ReplacedBy + " and delete it,"
		} else {
			msg += " Delete it,"
		}
		out = append(out, Finding{Path: registry.Path, Entry: entry,
			Message: msg + " or move remove_by with a reason."})
	}
	return out, nil
}

func anyFileMatches(files, patterns []string) bool {
	for _, f := range files {
		for _, p := range patterns {
			if registry.Match(p, f) {
				return true
			}
		}
	}
	return false
}
