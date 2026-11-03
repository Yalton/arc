package registry

import (
	"fmt"
	"regexp"
	"sort"

	"github.com/bmatcuk/doublestar/v4"
)

// Problem is one validation failure, tied to the registry entry involved.
type Problem struct {
	Entry   string `json:"entry"` // e.g. "concepts[goal].paths"
	Message string `json:"message"`
}

func (p Problem) String() string { return p.Entry + ": " + p.Message }

// Validate checks the registry against SPEC.md "Validation rules". files is
// the repository's file list (repo-relative, slash-separated), used for glob
// existence and ownership overlap.
func (r *Registry) Validate(files []string) []Problem {
	v := &validator{r: r, files: files}
	v.version()
	v.ids()
	v.references()
	v.globs()
	v.modules()
	v.overlap()
	v.dates()
	v.misc()
	return v.problems
}

type validator struct {
	r        *Registry
	files    []string
	problems []Problem
}

func (v *validator) add(entry, format string, args ...any) {
	v.problems = append(v.problems, Problem{Entry: entry, Message: fmt.Sprintf(format, args...)})
}

func (v *validator) version() {
	if v.r.Version != 1 {
		v.add("version", "unsupported version %d (want 1)", v.r.Version)
	}
}

func (v *validator) ids() {
	unique := func(section string, ids []string) {
		seen := map[string]bool{}
		for i, id := range ids {
			if id == "" {
				v.add(fmt.Sprintf("%s[%d]", section, i), "missing id")
				continue
			}
			if seen[id] {
				v.add(fmt.Sprintf("%s[%s]", section, id), "duplicate id %q", id)
			}
			seen[id] = true
		}
	}
	var ng, cs, bs, fs, rs, ks, ms []string
	for _, x := range v.r.NonGoals {
		ng = append(ng, x.ID)
	}
	for _, x := range v.r.Concepts {
		cs = append(cs, x.ID)
	}
	for _, x := range v.r.Boundaries {
		bs = append(bs, x.ID)
	}
	for _, x := range v.r.Flags {
		fs = append(fs, x.Name)
	}
	for _, x := range v.r.Removals {
		rs = append(rs, x.ID)
	}
	for _, x := range v.r.Modules {
		ms = append(ms, x.ID)
	}
	for _, x := range v.r.Checks {
		ks = append(ks, x.ID)
	}
	unique("non_goals", ng)
	unique("concepts", cs)
	unique("boundaries", bs)
	unique("checks", ks)
	unique("modules", ms)
	// Flags and removals share the arc:<entry-id> bead label namespace.
	unique("flags/removals", append(fs, rs...))
}

func (v *validator) references() {
	ref := func(entry, id string) {
		if id != "" && v.r.Concept(id) == nil {
			v.add(entry, "unknown concept %q", id)
		}
	}
	for _, f := range v.r.Flags {
		if f.Concept == "" {
			v.add("flags["+f.Name+"].concept", "missing concept")
		}
		ref("flags["+f.Name+"].concept", f.Concept)
	}
	for _, rm := range v.r.Removals {
		ref("removals["+rm.ID+"].replaced_by", rm.ReplacedBy)
	}
}

func (v *validator) globs() {
	check := func(entry string, patterns []string, mustMatch bool) {
		for _, p := range patterns {
			if !doublestar.ValidatePattern(p) {
				v.add(entry, "invalid glob %q", p)
				continue
			}
			if mustMatch && !v.matchesAny(p) {
				v.add(entry, "glob %q matches no files", p)
			}
		}
	}
	check("ignore", v.r.Ignore, false)
	for _, c := range v.r.Concepts {
		check("concepts["+c.ID+"].paths", c.Paths, true)
	}
	for _, b := range v.r.Boundaries {
		if len(b.From) == 0 || len(b.Deny) == 0 {
			v.add("boundaries["+b.ID+"]", "needs both from and deny")
		}
		check("boundaries["+b.ID+"].from", b.From, true)
		check("boundaries["+b.ID+"].deny", b.Deny, true)
	}
	for _, m := range v.r.Modules {
		check("modules["+m.ID+"].paths", m.Paths, true)
	}
	for _, rm := range v.r.Removals {
		// Exempt from existence: a finished removal is reported by gate.
		check("removals["+rm.ID+"].paths", rm.Paths, false)
	}
}

func (v *validator) matchesAny(pattern string) bool {
	for _, f := range v.files {
		if Match(pattern, f) {
			return true
		}
	}
	return false
}

// overlap reports each pair of concepts that own a common file, with the
// first such file and a count.
func (v *validator) overlap() {
	type pair struct{ a, b string }
	first := map[pair]string{}
	count := map[pair]int{}
	var order []pair
	for _, f := range v.files {
		var owners []string
		if _, ok := v.r.ModuleOf(f); ok {
			continue // module paths take precedence over concept paths
		}
		for _, c := range v.r.Concepts {
			if matchAny(c.Paths, f) {
				owners = append(owners, c.ID)
			}
		}
		for i := 0; i < len(owners); i++ {
			for j := i + 1; j < len(owners); j++ {
				p := pair{owners[i], owners[j]}
				if _, ok := first[p]; !ok {
					first[p] = f
					order = append(order, p)
				}
				count[p]++
			}
		}
	}
	for _, p := range order {
		msg := fmt.Sprintf("%s is owned by both %s and %s", first[p], p.a, p.b)
		if n := count[p]; n > 1 {
			msg += fmt.Sprintf(" (%d files overlap)", n)
		}
		v.add("concepts["+p.a+"].paths", "%s; narrow one so each file has one owner", msg)
	}
}

func (v *validator) dates() {
	for _, f := range v.r.Flags {
		if _, err := f.ExpiresAt(); err != nil {
			v.add("flags["+f.Name+"].expires", "date %q is not YYYY-MM-DD", f.Expires)
		}
	}
	for _, rm := range v.r.Removals {
		if _, err := rm.RemoveByAt(); err != nil {
			v.add("removals["+rm.ID+"].remove_by", "date %q is not YYYY-MM-DD", rm.RemoveBy)
		}
	}
}

func (v *validator) misc() {
	if v.r.Limits.FileLines < 0 || v.r.Limits.FunctionLines < 0 {
		v.add("limits", "limits must not be negative")
	}
	for _, k := range v.r.Checks {
		if len(k.Command) == 0 {
			v.add("checks["+k.ID+"].command", "missing command")
		}
	}
}

// SortProblems orders problems by entry then message, for stable output.
func SortProblems(ps []Problem) {
	sort.SliceStable(ps, func(i, j int) bool {
		if ps[i].Entry != ps[j].Entry {
			return ps[i].Entry < ps[j].Entry
		}
		return ps[i].Message < ps[j].Message
	})
}

var moduleID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// modules checks the shape of each module entry and that no file is under
// the paths of two modules. Ids and path globs are checked with the other
// sections (ids, globs).
func (v *validator) modules() {
	for _, m := range v.r.Modules {
		entry := "modules[" + m.ID + "]"
		if m.ID != "" && !moduleID.MatchString(m.ID) {
			v.add(entry, "id %q must match [A-Za-z0-9][A-Za-z0-9._-]*", m.ID)
		}
		if len(m.Entry) == 0 && m.Import == "" {
			v.add(entry, "needs at least one of entry and import")
		}
	}
	type pair struct{ a, b string }
	first := map[pair]string{}
	count := map[pair]int{}
	var order []pair
	for _, f := range v.files {
		var owners []string
		for _, m := range v.r.Modules {
			if matchAny(m.Paths, f) {
				owners = append(owners, m.ID)
			}
		}
		for i := 0; i < len(owners); i++ {
			for j := i + 1; j < len(owners); j++ {
				p := pair{owners[i], owners[j]}
				if _, ok := first[p]; !ok {
					first[p] = f
					order = append(order, p)
				}
				count[p]++
			}
		}
	}
	for _, p := range order {
		msg := fmt.Sprintf("%s is under the paths of both %s and %s", first[p], p.a, p.b)
		if n := count[p]; n > 1 {
			msg += fmt.Sprintf(" (%d files)", n)
		}
		v.add("modules["+p.a+"].paths", "%s; narrow one so each file has one module", msg)
	}
}
