package gate

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Yalton/arc/internal/registry"
)

const modulesRegistry = `version: 1
system: {name: demo, summary: Demo.}
ignore: ["*.md", ".arch/**"]
concepts:
  - {id: game, name: Game, summary: Game., paths: ["**"]}
modules:
  - {id: Kit, summary: Kit., paths: ["addons/Kit/**"], entry: [Kit]}
  - {id: Vend, summary: Vend., paths: ["addons/Vend/**"], entry: [Vend]}
limits: {file_lines: 10}
`

// submoduleRepo builds a repo whose addons/Kit is a real submodule of a
// local bare repository.
func submoduleRepo(t *testing.T) *testRepo {
	t.Helper()
	src := newRepo(t, "")
	src.write("a.cs", "class Kit {}\n")
	src.commitAll("kit")
	bare := filepath.Join(t.TempDir(), "kit.git")
	src.git("clone", "-q", "--bare", src.root, bare)
	if _, err := os.Stat(bare); err != nil {
		t.Fatal(err)
	}
	r := newRepo(t, modulesRegistry)
	r.write("Player.cs", "class Player {}\n")
	r.write("addons/Vend/x.cs", "class Vend {}\n")
	r.render()
	r.commitAll("init")
	r.git("-c", "protocol.file.allow=always", "submodule", "add", "-q", bare, "addons/Kit")
	return r
}

func TestModuleGitlinkNotJudgedByPlacement(t *testing.T) {
	r := submoduleRepo(t)
	r.git("add", "-A") // stages the gitlink and .gitmodules
	if got := r.git("ls-files", "-s", "addons/Kit"); !strings.HasPrefix(got, "160000") {
		t.Fatalf("not a gitlink: %q", got)
	}
	rep := r.gate("staged", "placement")
	if rep.Errors != 0 {
		t.Fatalf("unexpected:\n%s", findings(rep))
	}
	files, err := registry.RepoFiles(r.root)
	if err != nil {
		t.Fatal(err)
	}
	reg, _ := registry.Load(r.root)
	if ps := reg.Validate(files); len(ps) != 0 {
		t.Fatalf("validate with a gitlink: %v", ps)
	}
}

func TestSizeAndStructureSkipModulePaths(t *testing.T) {
	r := submoduleRepo(t)
	r.commitAll("sub")
	big := strings.Repeat("x\n", 30) + "CREATE TABLE vendored (id int);\n"
	r.write("addons/Vend/big.cs", big)
	r.write("Big.cs", big)
	r.git("add", "-A")
	got := findings(r.gate("staged", "size", "structure"))
	if strings.Contains(got, "addons/Vend") {
		t.Fatalf("module path judged:\n%s", got)
	}
	if !strings.Contains(got, "Big.cs") {
		t.Fatalf("project file should still be judged:\n%s", got)
	}
}

func TestCloneLinesDropsModuleToModuleOnly(t *testing.T) {
	reg, err := registry.Parse([]byte(modulesRegistry))
	if err != nil {
		t.Fatal(err)
	}
	in := []string{
		"addons/Kit/a.cs:1-9: duplicate of addons/Vend/x.cs:1-9",
		"addons/Kit/a.cs:1-9: duplicate of Player.cs:1-9",
	}
	got := cloneLines(DuplID, in, reg)
	if len(got) != 1 || !strings.Contains(got[0], "Player.cs") {
		t.Fatalf("got %v", got)
	}
}

// moduleLines returns the finding lines of rep that contain substr.
func moduleLines(rep *Report, substr string) []string {
	var out []string
	for _, f := range rep.Findings {
		if s := f.String(); strings.Contains(s, substr) {
			out = append(out, s)
		}
	}
	return out
}

const changed = "file changed under module path"

func TestModulesChangedFileUnderExistingModule(t *testing.T) {
	r := submoduleRepo(t)
	r.write("addons/Vend/y.cs", "class Y {}\n")
	r.commitAll("sub")
	r.git("checkout", "-q", "-b", "feat")
	r.write("addons/Vend/x.cs", "class Vend { int edited; }\n")
	r.git("add", "-A")
	got := moduleLines(r.gate("staged", "modules"), changed)
	want := "[modules] addons/Vend/x.cs: module Vend: " + changed
	if len(got) != 1 || got[0] != want {
		t.Fatalf("staged: got %q, want %q", got, want)
	}
	r.commitAll("edit")
	r.git("rm", "-q", "addons/Vend/y.cs")
	r.commitAll("delete")
	got = moduleLines(r.gate("main", "modules"), changed)
	if len(got) != 2 || !strings.Contains(got[0], "addons/Vend/x.cs") || !strings.Contains(got[1], "addons/Vend/y.cs") {
		t.Fatalf("base: got %q", got)
	}

	r.approved = true
	rep := r.gate("main", "modules")
	if got := moduleLines(rep, changed); len(got) != 0 {
		t.Fatalf("approved: got %q", got)
	}
	info := moduleLines(rep, "skipped: ARC_APPROVED=1 (pull request labelled arch-approved)")
	if len(info) != 1 || rep.Findings[0].Severity != Info {
		t.Fatalf("approved: want the approval info line first:\n%s", findings(rep))
	}
}

func TestModulesInstallingAModuleIsNotAFinding(t *testing.T) {
	withoutVend := strings.Replace(modulesRegistry, "  - {id: Vend, summary: Vend., paths: [\"addons/Vend/**\"], entry: [Vend]}\n", "", 1)
	if withoutVend == modulesRegistry {
		t.Fatal("fixture: Vend entry not removed")
	}
	r := newRepo(t, withoutVend)
	r.write("Player.cs", "class Player {}\n")
	r.commitAll("init")
	r.write(registry.Path, modulesRegistry)
	r.write("addons/Vend/x.cs", "class Vend {}\n")
	r.git("add", "-A")
	if got := moduleLines(r.gate("staged", "modules"), changed); len(got) != 0 {
		t.Fatalf("installing a module: got %q", got)
	}
}

func TestModulesAllSkipsChangedFiles(t *testing.T) {
	r := submoduleRepo(t)
	r.commitAll("sub")
	if got := moduleLines(r.gate("all", "modules"), changed); len(got) != 0 {
		t.Fatalf("--all: got %q", got)
	}
}

func TestModulesDirtySubmodule(t *testing.T) {
	r := submoduleRepo(t)
	r.commitAll("sub")
	r.write("addons/Kit/a.cs", "class Kit { int edited; }\n")
	rep := r.gate("working", "modules")
	got := moduleLines(rep, "submodule has local changes")
	want := "[modules] addons/Kit: module Kit: submodule has local changes"
	if len(got) != 1 || got[0] != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got := moduleLines(rep, changed); len(got) != 0 {
		t.Fatalf("the gitlink is not a changed file: %q", got)
	}
}

func TestModulesMovedSubmodulePointerIsNotAFinding(t *testing.T) {
	r := submoduleRepo(t)
	r.commitAll("sub")
	r.write("addons/Kit/b.cs", "class B {}\n")
	r.git("-C", "addons/Kit", "add", "-A")
	r.git("-C", "addons/Kit", "commit", "-q", "-m", "upgrade")
	r.git("add", "addons/Kit")
	rep := r.gate("staged", "modules")
	if got := append(moduleLines(rep, changed), moduleLines(rep, "submodule")...); len(got) != 0 {
		t.Fatalf("pointer move: got %q", got)
	}
}

func TestModulesSubmoduleNotCheckedOutIsSkipped(t *testing.T) {
	r := submoduleRepo(t)
	r.commitAll("sub")
	r.git("submodule", "deinit", "-q", "-f", "addons/Kit")
	if got := moduleLines(r.gate("all", "modules"), "submodule"); len(got) != 0 {
		t.Fatalf("got %q", got)
	}
}

const goModuleRegistry = `version: 1
system: {name: demo, summary: Demo.}
concepts:
  - {id: app, name: App, summary: App., paths: ["**"]}
modules:
  - {id: M, summary: M., import: example.com/m}
`

func TestModulesGoModReplace(t *testing.T) {
	r := newRepo(t, goModuleRegistry)
	r.write("go.mod", "module example.com/app\n\nreplace example.com/m => ../m\n")
	r.commitAll("init")
	got := moduleLines(r.gate("all", "modules"), "replaces")
	want := "[modules] go.mod: module M: go.mod replaces example.com/m"
	if len(got) != 1 || got[0] != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestReplaceSources(t *testing.T) {
	gomod := `module example.com/app

require example.com/x v1.0.0 // replace a => b
replace example.com/one v1.2.3 => ../one
replace (
	// example.com/commented => ../c
	"example.com/two" => ../two
	example.com/three v0.1.0 => example.com/fork v0.1.1
)
`
	got := strings.Join(replaceSources([]byte(gomod)), " ")
	if want := "example.com/one example.com/two example.com/three"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

const wiringRegistry = `version: 1
system: {name: demo, summary: Demo.}
ignore: ["gen/**"]
concepts:
  - {id: game, name: Game, summary: Game., paths: ["**"]}
modules:
  - {id: Cursor, summary: C., paths: ["addons/Kit/**"], entry: [InputModeManager.Instance]}
`

func TestModulesWiringByEntry(t *testing.T) {
	const ref = "InputModeManager.Instance.Push(this);\n"
	cases := []struct {
		name      string
		files     map[string]string
		untracked map[string]string
		wired     bool
	}{
		{"entry in Main.cs", map[string]string{"Main.cs": ref}, nil, true},
		{"autoload line in project.godot", map[string]string{"project.godot": "[autoload]\nInputModeManager=\"*res://addons/Kit/InputModeManager.cs\"\n"}, nil, true},
		{"only inside module paths", nil, nil, false},
		{"only in a registry-ignored file", map[string]string{"gen/x.cs": ref}, nil, false},
		{"only in a git-ignored file", map[string]string{".gitignore": "build/\n"}, map[string]string{"build/x.cs": ref}, false},
		{"only in Markdown", map[string]string{"docs/MODULES.md": ref}, nil, false},
		{"only in a binary file", map[string]string{"blob.bin": "InputModeManager\x00"}, nil, false},
		{"not a whole word", map[string]string{"Main.cs": "MyInputModeManager x;\n"}, nil, false},
		{"untracked, not ignored", nil, map[string]string{"Untracked.cs": ref}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newRepo(t, wiringRegistry)
			r.write("addons/Kit/InputModeManager.cs", "class InputModeManager { public static InputModeManager Instance; }\n")
			for p, s := range c.files {
				r.write(p, s)
			}
			r.commitAll("init")
			for p, s := range c.untracked {
				r.write(p, s)
			}
			got := moduleLines(r.gate("all", "modules"), "not wired")
			want := "[modules] module Cursor: not wired (no reference to InputModeManager.Instance outside module paths)"
			switch {
			case c.wired && len(got) != 0:
				t.Fatalf("want wired, got %q", got)
			case !c.wired && (len(got) != 1 || got[0] != want):
				t.Fatalf("got %q, want %q", got, want)
			}
		})
	}
}

func TestModulesWiringByGoImport(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not on PATH")
	}
	r := newRepo(t, `version: 1
system: {name: demo, summary: Demo.}
concepts:
  - {id: app, name: App, summary: App., paths: ["**"]}
modules:
  - {id: Kit, summary: K., import: example.com/app/kit}
  - {id: Unused, summary: U., import: example.com/app/unused}
`)
	r.write("go.mod", "module example.com/app\n\ngo 1.21\n")
	r.write("main.go", "package main\n\nimport _ \"example.com/app/kit/sub\"\n\nfunc main() {}\n")
	r.write("kit/sub/sub.go", "package sub\n")
	r.write("unused/unused.go", "package unused\n")
	r.commitAll("init")
	got := moduleLines(r.gate("all", "modules"), "not wired")
	want := "[modules] module Unused: not wired (no reference to import example.com/app/unused outside module paths)"
	if len(got) != 1 || got[0] != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestModulesWiringGoListFailureIsAWarning(t *testing.T) {
	r := newRepo(t, goModuleRegistry) // no go.mod: go list fails
	r.write("x.txt", "text\n")
	r.commitAll("init")
	rep := r.gate("all", "modules")
	want := "[modules] warning: module M: wiring not checked (go list failed)"
	if got := findings(rep); got != want || rep.Errors != 0 || rep.Warnings != 1 {
		t.Fatalf("got %q, want %q", got, want)
	}
}
