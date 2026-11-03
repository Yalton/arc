package gate

import (
	"os"
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
