package gate

import (
	"fmt"
	"slices"
	"strconv"

	"github.com/Yalton/arc/internal/registry"
)

// ApprovedEnv is the environment variable CI sets to 1 when a maintainer
// labelled the pull request ApprovalLabel; the approval check then passes.
const (
	ApprovedEnv   = "ARC_APPROVED"
	ApprovalLabel = "arch-approved"
)

// checkApproval reports structural registry changes an agent could make to
// approve its own work: added or promoted concepts, removed or loosened
// boundaries, removed rules, extended flag expiries and removal deadlines,
// widened concept paths, a widened ignore list and raised or removed size
// limits. It runs only with --base (CI on pull
// requests), so local modes can still propose such changes, and only when
// the base has a registry (adoption is not judged). Widening is judged on
// the files in the tree: a pattern set widens when it now matches a file it
// did not.
func checkApproval(ctx *Context) ([]Finding, error) {
	if !ctx.Base || ctx.BaseReg == nil {
		return nil, nil
	}
	if ctx.Approved {
		return []Finding{{
			Severity: Info,
			Path:     registry.Path,
			Message:  "skipped: " + ApprovedEnv + "=1 (pull request labelled " + ApprovalLabel + ")",
		}}, nil
	}
	a := approval{files: ctx.Files}
	a.concepts(ctx.BaseReg, ctx.Reg)
	a.boundaries(ctx.BaseReg, ctx.Reg)
	a.deadlines(ctx.BaseReg, ctx.Reg)
	a.loosened(ctx.BaseReg, ctx.Reg)
	return a.out, nil
}

type approval struct {
	files []string
	out   []Finding
}

func (a *approval) add(entry, format string, args ...any) {
	a.out = append(a.out, Finding{
		Path:  registry.Path,
		Entry: entry,
		Message: "needs human approval: " + fmt.Sprintf(format, args...) +
			". A maintainer approves by adding the " + ApprovalLabel + " label to the pull request.",
	})
}

func (a *approval) concepts(base, head *registry.Registry) {
	for _, c := range head.Concepts {
		old := base.Concept(c.ID)
		switch {
		case old == nil:
			a.add("concepts["+c.ID+"]", "concept %s added", c.ID)
			continue
		case old.Draft && !c.Draft:
			a.add("concepts["+c.ID+"].draft", "draft concept %s promoted to a concept", c.ID)
		}
		if f, n := widened(a.files, old.Paths, c.Paths); f != "" {
			a.add("concepts["+c.ID+"].paths", "concept %s paths widened to own %s%s", c.ID, f, more(n))
		}
	}
	for _, old := range base.Concepts {
		c := head.Concept(old.ID)
		for _, rule := range old.Rules {
			if c == nil || !slices.Contains(c.Rules, rule) {
				a.add("concepts["+old.ID+"].rules", "rule removed from concept %s: %s", old.ID, strconv.Quote(rule))
			}
		}
	}
}

func (a *approval) boundaries(base, head *registry.Registry) {
	for _, old := range base.Boundaries {
		b := boundary(head, old.ID)
		if b == nil {
			a.add("boundaries["+old.ID+"]", "boundary %s removed", old.ID)
			continue
		}
		if f, n := widened(a.files, b.Deny, old.Deny); f != "" {
			a.add("boundaries["+old.ID+"].deny", "boundary %s no longer denies %s%s", old.ID, f, more(n))
		}
		if f, n := widened(a.files, old.From, b.From); f != "" {
			a.add("boundaries["+old.ID+"].from", "boundary %s from widened to %s%s", old.ID, f, more(n))
		}
	}
}

// deadlines reports flags whose expiry and removals whose remove_by moved
// later. Unparseable dates are left to validation.
func (a *approval) deadlines(base, head *registry.Registry) {
	for _, old := range base.Flags {
		for _, f := range head.Flags {
			if f.Name != old.Name {
				continue
			}
			was, err1 := old.ExpiresAt()
			now, err2 := f.ExpiresAt()
			if err1 == nil && err2 == nil && now.After(was) {
				a.add("flags["+f.Name+"].expires", "flag %s expiry extended from %s to %s", f.Name, old.Expires, f.Expires)
			}
		}
	}
	for _, old := range base.Removals {
		for _, rm := range head.Removals {
			if rm.ID != old.ID {
				continue
			}
			was, err1 := old.RemoveByAt()
			now, err2 := rm.RemoveByAt()
			if err1 == nil && err2 == nil && now.After(was) {
				a.add("removals["+rm.ID+"].remove_by", "removal %s remove_by extended from %s to %s", rm.ID, old.RemoveBy, rm.RemoveBy)
			}
		}
	}
}

// loosened reports an ignore list that now exempts a file it did not (an
// ignored file escapes placement, size and structure) and size limits
// raised or removed (0 means no limit).
func (a *approval) loosened(base, head *registry.Registry) {
	ignored := func(r *registry.Registry) []string {
		return append(slices.Clone(registry.BuiltinIgnore), r.Ignore...)
	}
	if f, n := widened(a.files, ignored(base), ignored(head)); f != "" {
		a.add("ignore", "ignore widened to exempt %s%s", f, more(n))
	}
	limit := func(key string, was, now int) {
		switch {
		case was <= 0 || now == was || (now > 0 && now < was):
		case now <= 0:
			a.add("limits."+key, "limit %s removed (was %d)", key, was)
		default:
			a.add("limits."+key, "limit %s raised from %d to %d", key, was, now)
		}
	}
	limit("file_lines", base.Limits.FileLines, head.Limits.FileLines)
	limit("function_lines", base.Limits.FunctionLines, head.Limits.FunctionLines)
}

func boundary(r *registry.Registry, id string) *registry.Boundary {
	for i := range r.Boundaries {
		if r.Boundaries[i].ID == id {
			return &r.Boundaries[i]
		}
	}
	return nil
}

// widened returns the first file (in files order) matched by after but not
// by before, and how many more there are.
func widened(files, before, after []string) (string, int) {
	first, n := "", 0
	for _, f := range files {
		if matchesAny(after, f) && !matchesAny(before, f) {
			if first == "" {
				first = f
			} else {
				n++
			}
		}
	}
	return first, n
}

func matchesAny(patterns []string, f string) bool {
	for _, p := range patterns {
		if registry.Match(p, f) {
			return true
		}
	}
	return false
}

func more(n int) string {
	if n == 0 {
		return ""
	}
	return fmt.Sprintf(" (and %d more file(s))", n)
}
