package gate

import (
	"strings"

	"github.com/Yalton/arc/internal/brief"
)

// checkPlacement fails every added, non-ignored file no concept owns.
// Only the change set is judged, so pre-existing unowned files pass.
func checkPlacement(ctx *Context) ([]Finding, error) {
	var out []Finding
	for _, path := range ctx.Changes.Added() {
		if ctx.Reg.IsIgnored(path) || ctx.Reg.Owner(path) != nil {
			continue
		}
		out = append(out, Finding{
			Path:    path,
			Entry:   "concepts",
			Message: strings.TrimPrefix(brief.PlaceMessage(brief.Place(ctx.Reg, path)), path+": "),
		})
	}
	return out, nil
}
