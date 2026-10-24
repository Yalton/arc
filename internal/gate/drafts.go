package gate

import "strings"

// checkDrafts warns while concepts proposed by 'arc init --scan' remain drafts.
func checkDrafts(ctx *Context) ([]Finding, error) {
	var ids []string
	for _, c := range ctx.Reg.Concepts {
		if c.Draft {
			ids = append(ids, c.ID)
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	return []Finding{{
		Severity: Warning,
		Entry:    "concepts",
		Message: "draft concepts: " + strings.Join(ids, ", ") +
			". Refine summary, rules and paths, then remove 'draft: true'.",
	}}, nil
}
