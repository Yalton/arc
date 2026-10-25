package main

import (
	"fmt"
	"strings"

	"github.com/Yalton/arc/internal/symbols"
)

// runFind ranks concepts and exported symbols against the query words so
// an agent checks for existing code before building new (SPEC.md, "arc
// find"). It exits 1 when nothing matches.
func runFind(out *output, args []string) int {
	query := strings.TrimSpace(strings.Join(args, " "))
	if query == "" {
		return out.fail(exitUsage, "find: need words describing what you are looking for")
	}
	r, err := openRepo()
	if err != nil {
		return out.fail(exitUsage, err.Error())
	}
	provides, err := symbols.ByConcept(r.reg, r.root, r.files, nil)
	if err != nil {
		return out.fail(exitUsage, "find: reading symbols: "+err.Error())
	}
	res := symbols.Find(r.reg, provides, query)
	if len(res.Concepts) == 0 && len(res.Symbols) == 0 {
		out.result("find: nothing matches "+fmt.Sprintf("%q", query)+"; no existing concept or exported symbol covers it", res)
		return exitViolations
	}
	out.result(findText(res), res)
	return exitOK
}

func findText(res symbols.FindResult) string {
	var w strings.Builder
	if len(res.Concepts) > 0 {
		w.WriteString("Concepts:\n")
		for _, c := range res.Concepts {
			fmt.Fprintf(&w, "- %s", c.ID)
			if c.Name != "" {
				fmt.Fprintf(&w, " (%s)", c.Name)
			}
			if c.Summary != "" {
				w.WriteString(": " + c.Summary)
			}
			fmt.Fprintf(&w, " [%s]\n", strings.Join(c.Why, ", "))
		}
	}
	if len(res.Symbols) > 0 {
		w.WriteString("Symbols:\n")
		for _, s := range res.Symbols {
			fmt.Fprintf(&w, "- %s:%d %s", s.File, s.Line, s.Name)
			if s.Doc != "" {
				w.WriteString(" - " + s.Doc)
			}
			fmt.Fprintf(&w, " (%s)\n", s.Concept)
		}
	}
	return strings.TrimRight(w.String(), "\n")
}
