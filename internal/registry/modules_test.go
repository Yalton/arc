package registry

import (
	"strings"
	"testing"
)

const modulesHead = `version: 1
system: {name: demo}
concepts:
  - id: game
    paths: ["**"]
`

func parseValidate(t *testing.T, yaml string, files []string) (*Registry, []Problem) {
	t.Helper()
	r, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	return r, r.Validate(files)
}

func wantProblem(t *testing.T, ps []Problem, sub string) {
	t.Helper()
	for _, p := range ps {
		if strings.Contains(p.String(), sub) {
			return
		}
	}
	t.Fatalf("want a problem containing %q, got %v", sub, ps)
}

func TestModuleUnknownKeyIsError(t *testing.T) {
	_, err := Parse([]byte(modulesHead + "modules:\n  - {id: Kit, entry: [Kit], bogus: 1}\n"))
	if err == nil || !strings.Contains(err.Error(), "bogus") {
		t.Fatalf("want unknown key error, got %v", err)
	}
}

func TestModuleValidation(t *testing.T) {
	files := []string{"Player.cs", "addons/Kit/a.cs", "addons/Other/b.cs"}
	_, ps := parseValidate(t, modulesHead+`modules:
  - {id: Kit, paths: ["addons/Kit/**"], entry: [Kit]}
  - {id: Kit, paths: ["addons/Other/**"], entry: [Other]}
  - {id: Bad id, entry: [X]}
  - {id: Ghost, paths: ["addons/Nope/**"], entry: [Ghost]}
  - {id: NoEntry, paths: ["addons/Other/**"]}
`, files)
	wantProblem(t, ps, `modules[Kit]: duplicate id "Kit"`)
	wantProblem(t, ps, `modules[Bad id]: id "Bad id" must match`)
	wantProblem(t, ps, `modules[Ghost].paths: glob "addons/Nope/**" matches no files`)
	wantProblem(t, ps, "modules[NoEntry]: needs at least one of entry and import")
	wantProblem(t, ps, "addons/Other/b.cs is under the paths of both Kit and NoEntry")
}

func TestModuleGoModuleWithoutPaths(t *testing.T) {
	_, ps := parseValidate(t, modulesHead+"modules:\n  - {id: lib, import: example.com/lib}\n", []string{"Player.cs"})
	if len(ps) != 0 {
		t.Fatalf("unexpected: %v", ps)
	}
}

// A gitlink shows up in the file list as its directory; dir/** must match it.
func TestModuleGitlinkMatchesDirGlob(t *testing.T) {
	if !Match("addons/Kit/**", "addons/Kit") {
		t.Fatal("doublestar: addons/Kit/** should match addons/Kit")
	}
	_, ps := parseValidate(t, modulesHead+`modules:
  - {id: Kit, paths: ["addons/Kit/**"], entry: [Kit]}
`, []string{"Player.cs", "addons/Kit"})
	if len(ps) != 0 {
		t.Fatalf("unexpected: %v", ps)
	}
}

func TestModulePrecedenceOverConcept(t *testing.T) {
	r, ps := parseValidate(t, modulesHead+`  - id: addons
    paths: ["addons/**"]
modules:
  - {id: Kit, paths: ["addons/Kit/**"], entry: [Kit]}
`, []string{"Player.cs", "addons/Kit/a.cs"})
	if len(ps) != 0 {
		t.Fatalf("concept and module must not overlap: %v", ps)
	}
	if r.Owner("addons/Kit/a.cs") != nil {
		t.Fatal("Owner under a module must be nil")
	}
	if m, ok := r.ModuleOf("addons/Kit/a.cs"); !ok || m.ID != "Kit" {
		t.Fatalf("ModuleOf = %v, %v", m, ok)
	}
	if c := r.Owner("Player.cs"); c == nil || c.ID != "game" {
		t.Fatalf("Owner(Player.cs) = %v", c)
	}
	if _, ok := r.ModuleOf("Player.cs"); ok {
		t.Fatal("Player.cs is not a module path")
	}
}

func TestGitmodulesIsBuiltinIgnore(t *testing.T) {
	r := &Registry{}
	if !r.IsIgnored(".gitmodules") {
		t.Fatal(".gitmodules should be ignored")
	}
}
