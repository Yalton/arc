package gate

import (
	"strings"
	"time"

	"arc/internal/registry"
)

// checkRemovals fails removals past their deadline, and removals whose
// paths are all gone but whose entry remains.
func checkRemovals(ctx *Context) ([]Finding, error) {
	today := ctx.Now.UTC().Format(registry.DateLayout)
	var out []Finding
	for _, rm := range ctx.Reg.Removals {
		entry := "removals[" + rm.ID + "]"
		if RemovalDone(rm, ctx.Files) {
			out = append(out, Finding{Path: registry.Path, Entry: entry,
				Message: "removal " + rm.ID + " is done (no files match " + strings.Join(rm.Paths, ", ") +
					"). Delete the entry."})
			continue
		}
		if !overdue(rm, today) {
			continue
		}
		what := rm.What
		if what == "" {
			what = rm.ID
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

// RemovalDone reports whether rm lists paths and no file matches them any
// more, so only the registry entry remains.
func RemovalDone(rm registry.Removal, files []string) bool {
	return len(rm.Paths) > 0 && !anyFileMatches(files, rm.Paths)
}

// OverdueRemovals returns the removals past remove_by that are not done,
// in registry order. Unparseable dates are left to the registry check.
func OverdueRemovals(reg *registry.Registry, files []string, now time.Time) []registry.Removal {
	today := now.UTC().Format(registry.DateLayout)
	var out []registry.Removal
	for _, rm := range reg.Removals {
		if !RemovalDone(rm, files) && overdue(rm, today) {
			out = append(out, rm)
		}
	}
	return out
}

// overdue reports whether rm's valid remove_by date is before today.
func overdue(rm registry.Removal, today string) bool {
	_, err := rm.RemoveByAt()
	return err == nil && rm.RemoveBy < today
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
