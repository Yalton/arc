package audit

import (
	_ "embed"
	"strings"
	"text/template"
	"time"

	"github.com/Yalton/arc/internal/brief"
	"github.com/Yalton/arc/internal/registry"
)

//go:embed prompt.tmpl
var promptText string

var promptTmpl = template.Must(template.New("prompt").Parse(promptText))

// Prompt returns the architect prompt with the report and the whole
// registry brief embedded (SPEC.md, Principle 1). arc only prints it; the
// caller decides which agent runs it. The output depends only on its
// arguments.
func Prompt(rep *Report, reg *registry.Registry, now time.Time) string {
	system := reg.System.Name
	if system == "" {
		system = "this repository"
	}
	var b strings.Builder
	err := promptTmpl.Execute(&b, map[string]string{
		"System":       system,
		"RegistryPath": registry.Path,
		"Report":       rep.Text(),
		"Brief":        strings.TrimSuffix(brief.Whole(reg, now).Text(), "\n"),
	})
	if err != nil {
		panic("audit: prompt template: " + err.Error()) // the template is fixed; a failure is a bug
	}
	return strings.TrimSuffix(b.String(), "\n")
}
