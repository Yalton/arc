package brief

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/Yalton/arc/internal/registry"
)

func withModules(t *testing.T) *registry.Registry {
	t.Helper()
	reg := loadFixture(t)
	reg.Modules = []registry.Module{
		{ID: "CursorKit", Summary: "Custom mouse cursors for Godot.", Paths: []string{"addons/CursorKit/**"},
			Covers: []string{"custom mouse cursor", "cursor shapes"}, Entry: []string{"CursorKit.set_cursor", "CursorKitAutoload"},
			Rules: []string{"Use set_cursor.", "Never subclass."}},
		{ID: "retry", Summary: "Retry with backoff.", Import: "example.com/retry", Covers: []string{"retry with backoff"}},
		{ID: "plain", Summary: "Nothing special."},
	}
	return reg
}

func TestWholeModules(t *testing.T) {
	b := Whole(withModules(t), now)
	golden(t, "whole-modules.txt", b.Text())
	if b.ModulesMore != 0 || len(b.Modules) != 3 {
		t.Fatalf("modules %d more %d", len(b.Modules), b.ModulesMore)
	}
	if strings.Contains(Whole(loadFixture(t), now).Text(), "Modules (installed") {
		t.Fatal("Modules section without modules")
	}
	js, _ := json.Marshal(Whole(loadFixture(t), now))
	if strings.Contains(string(js), "modules") {
		t.Fatalf("modules fields without modules: %s", js)
	}
}

func TestModulesCap(t *testing.T) {
	reg := loadFixture(t)
	for i := 0; i < 20; i++ {
		reg.Modules = append(reg.Modules, registry.Module{ID: fmt.Sprintf("m%02d", i), Summary: strings.Repeat("word ", 60)})
	}
	b := Whole(reg, now)
	text := b.ModulesText()
	if n := len(text[:strings.Index(text, "(")]); n/4 > ModulesTokenCap {
		t.Fatalf("section is %d tokens", n/4)
	}
	if b.ModulesMore == 0 || b.ModulesMore+len(b.Modules) != 20 || b.Modules[0].ID != "m00" {
		t.Fatalf("shown %d more %d", len(b.Modules), b.ModulesMore)
	}
	want := fmt.Sprintf("(%d more: arc brief --modules)\n", b.ModulesMore)
	if !strings.HasSuffix(text, want) {
		t.Fatalf("missing %q in %q", want, text[len(text)-60:])
	}
	// The first module is shown even when it alone passes the cap.
	reg.Modules = []registry.Module{{ID: "big", Summary: strings.Repeat("x ", 3000)}, {ID: "small"}}
	b = Whole(reg, now)
	if len(b.Modules) != 1 || b.Modules[0].ID != "big" || b.ModulesMore != 1 {
		t.Fatalf("shown %d more %d", len(b.Modules), b.ModulesMore)
	}
	if all := AllModules(reg); len(all.Modules) != 2 || all.ModulesMore != 0 {
		t.Fatalf("AllModules shows %d", len(all.Modules))
	}
}

func TestScopedModules(t *testing.T) {
	reg := withModules(t)
	// An owned path in a module lists that module only.
	b := ForPaths(reg, []string{"addons/CursorKit/x.gd"}, now)
	if got := moduleIDs(b); got != "CursorKit" {
		t.Fatalf("owner: %s", got)
	}
	// A module whose covers words appear in the concept summary.
	reg.Concepts[0].Summary = "Retry failed goal runs with exponential backoff."
	b = ForPaths(reg, []string{"internal/workgraph/goal.go"}, now)
	if got := moduleIDs(b); got != "retry" {
		t.Fatalf("covers: %s", got)
	}
	b, _ = ForConcepts(reg, []string{"goal"}, now)
	if got := moduleIDs(b); got != "retry" {
		t.Fatalf("concept covers: %s", got)
	}
	// Bead text: an entry's leading identifier as a whole word, and covers.
	b.AddModuleText(reg, "wire CursorKit into the menu; need custom mouse cursor")
	if got := moduleIDs(b); got != "CursorKit,retry" {
		t.Fatalf("bead: %s", got)
	}
	b.AddModuleText(reg, "CursorKitten is unrelated; one cursor only")
	if got := moduleIDs(b); got != "retry" {
		t.Fatalf("partial words: %s", got)
	}
	// Nothing relevant: no section.
	reg.Concepts[0].Summary = "The only goal."
	b = ForPaths(reg, []string{"internal/workgraph/goal.go"}, now)
	if len(b.Modules) != 0 || strings.Contains(b.Text(), "Modules (installed") {
		t.Fatalf("unexpected modules: %s", moduleIDs(b))
	}
}

func moduleIDs(b Brief) string {
	var ids []string
	for _, m := range b.Modules {
		ids = append(ids, m.ID)
	}
	return strings.Join(ids, ",")
}
