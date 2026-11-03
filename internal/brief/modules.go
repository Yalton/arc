package brief

import (
	"fmt"
	"strings"

	"github.com/Yalton/arc/internal/registry"
	"github.com/Yalton/arc/internal/symbols"
)

// ModulesTokenCap caps the Modules section of a brief (SPEC.md, "arc
// brief"), counted like Tokens: text length / 4.
const ModulesTokenCap = 800

// MatchesModule reports whether m matches text by SPEC.md's "Word
// matching": a covers phrase matches when at least two distinct tokens of
// it, and at least half, occur in text; an entry matches when its leading
// identifier occurs in text as a whole word, case-insensitively.
func MatchesModule(m registry.Module, text string) bool {
	if MatchesCovers(m, text) {
		return true
	}
	idents := map[string]bool{}
	for _, w := range strings.FieldsFunc(text, func(r rune) bool {
		return !(r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r > 127)
	}) {
		idents[strings.ToLower(w)] = true
	}
	for _, e := range m.Entry {
		if i := strings.IndexByte(e, '.'); i >= 0 {
			e = e[:i]
		}
		if e != "" && idents[strings.ToLower(e)] {
			return true
		}
	}
	return false
}

// MatchesCovers reports whether one of m's covers phrases matches text:
// at least two distinct tokens of it, and at least half, occur in text.
// Triage uses this alone, since a bead that names an entry is wiring the
// module, not rebuilding it.
func MatchesCovers(m registry.Module, text string) bool {
	have := map[string]bool{}
	for _, t := range symbols.Tokens(text) {
		have[t] = true
	}
	for _, phrase := range m.Covers {
		seen := map[string]bool{}
		hit := 0
		for _, t := range symbols.Tokens(phrase) {
			if !seen[t] {
				seen[t] = true
				if have[t] {
					hit++
				}
			}
		}
		if hit >= 2 && hit*2 >= len(seen) {
			return true
		}
	}
	return false
}

// scopedModules picks, in registry order, the modules that own one of
// paths or match the owning concepts' summaries and extra text.
func scopedModules(reg *registry.Registry, paths []string, concepts []ConceptEntry, extra string) []registry.Module {
	var text []string
	for _, c := range concepts {
		text = append(text, c.Summary)
	}
	text = append(text, extra)
	all := strings.Join(text, "\n")
	owning := map[string]bool{}
	for _, p := range paths {
		if m, ok := reg.ModuleOf(p); ok {
			owning[m.ID] = true
		}
	}
	var out []registry.Module
	for _, m := range reg.Modules {
		if owning[m.ID] || MatchesModule(m, all) {
			out = append(out, m)
		}
	}
	return out
}

// AddModuleText adds the modules whose covers or entry match text (a
// bead's text) to the brief's Modules, keeping registry order.
func (b *Brief) AddModuleText(reg *registry.Registry, text string) {
	var paths []string
	for _, p := range b.Paths {
		paths = append(paths, p.Path)
	}
	b.setModules(scopedModules(reg, paths, b.Concepts, text))
}

// setModules stores the modules that fit the cap and counts the rest.
func (b *Brief) setModules(mods []registry.Module) {
	b.Modules, b.ModulesMore = capModules(mods, ModulesTokenCap)
}

// capModules keeps modules in order until the next would pass the token
// cap (the header line counts); the first is always kept.
func capModules(mods []registry.Module, cap int) ([]registry.Module, int) {
	if len(mods) == 0 {
		return nil, 0
	}
	size := len(modulesHeader)
	for i, m := range mods {
		size += len(moduleText(m))
		if i > 0 && size/4 > cap {
			return mods[:i], len(mods) - i
		}
	}
	return mods, 0
}

const modulesHeader = "Modules (installed; use, never edit):\n"

// moduleText renders one module: id and summary, then entry and rules.
func moduleText(m registry.Module) string {
	var w strings.Builder
	w.WriteString(m.ID)
	if s := oneLine(m.Summary); s != "" {
		w.WriteString(" — " + s)
	}
	w.WriteString("\n")
	if s := m.Surface(); s != "" {
		w.WriteString("  entry: " + s + "\n")
	}
	if len(m.Rules) > 0 {
		rules := make([]string, len(m.Rules))
		for i, r := range m.Rules {
			rules[i] = oneLine(r)
		}
		w.WriteString("  rules: " + strings.Join(rules, "; ") + "\n")
	}
	return w.String()
}

func (b Brief) writeModules(w *strings.Builder) {
	if len(b.Modules) == 0 {
		return
	}
	w.WriteString(modulesHeader)
	for _, m := range b.Modules {
		w.WriteString(moduleText(m))
	}
	if b.ModulesMore > 0 {
		fmt.Fprintf(w, "(%d more: arc brief --modules)\n", b.ModulesMore)
	}
}

// AllModules is the uncapped list `arc brief --modules` prints.
func AllModules(reg *registry.Registry) Brief {
	return Brief{Modules: reg.Modules}
}

// ModulesText renders only the Modules section of b ("" when empty).
func (b Brief) ModulesText() string {
	var w strings.Builder
	b.writeModules(&w)
	return w.String()
}
