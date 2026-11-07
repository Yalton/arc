package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// goldenDir is absolute: tests chdir into temp repos.
var goldenDir, _ = filepath.Abs(filepath.Join("..", "..", "testdata", "registry"))

const moduleRegistry = `# Registry for the demo project.
version: 1

system:
  name: demo # the name
  summary: Demo.

concepts:
  # the game itself
  - id: game
    name: Game
    summary: The game.
    paths: ["src/**"]

limits:
  file_lines: 1000 # keep files short
`

// moduleRepo makes a git repo with registry content, some files, chdirs in.
func moduleRepo(t *testing.T, registryYAML string, files ...string) string {
	t.Helper()
	root := t.TempDir()
	cmd := exec.Command("git", "init", "-q", root)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	all := append([]string{".arch/registry.yaml"}, files...)
	for _, rel := range all {
		content := "x\n"
		if rel == ".arch/registry.yaml" {
			content = registryYAML
		}
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(root)
	return root
}

func readFile(t *testing.T, root, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	p := filepath.Join(goldenDir, name)
	if os.Getenv("ARC_UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(p, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != got {
		t.Fatalf("%s mismatch\n--- want\n%s\n--- got\n%s", name, want, got)
	}
}

func TestModuleSetKeepsComments(t *testing.T) {
	root := moduleRepo(t, moduleRegistry, "src/a.go", "addons/Kit/k.gd")
	code, out, errs := runArc("module", "set", "Kit", "--summary", "A kit.", "--path", "addons/Kit/**",
		"--covers", "widgets", "--entry", "Kit", "--rule", "Never edit.", "--source", "dir:../Kit")
	if code != exitOK {
		t.Fatalf("code %d out %q err %q", code, out, errs)
	}
	golden(t, "module-set-commented.yaml", readFile(t, root, ".arch/registry.yaml"))
}

func TestModuleSetReplacesInPlace(t *testing.T) {
	root := moduleRepo(t, moduleRegistry, "src/a.go", "addons/Kit/k.gd", "addons/B/b.gd")
	for _, args := range [][]string{
		{"module", "set", "Kit", "--summary", "A kit.", "--path", "addons/Kit/**", "--entry", "Kit", "--covers", "widgets"},
		{"module", "set", "B", "--summary", "B.", "--path", "addons/B/**", "--entry", "B"},
		{"module", "set", "Kit", "--summary", "Kit v2.", "--path", "addons/Kit/**", "--entry", "Kit"},
	} {
		if code, out, errs := runArc(args...); code != exitOK {
			t.Fatalf("%v: code %d %q %q", args, code, out, errs)
		}
	}
	code, out, _ := runArc("module", "list", "--json")
	var got struct {
		Modules []struct {
			ID, Summary string
			Covers      []string
		}
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil || code != exitOK {
		t.Fatalf("list: %v %q", err, out)
	}
	if len(got.Modules) != 2 || got.Modules[0].ID != "Kit" || got.Modules[0].Summary != "Kit v2." ||
		got.Modules[0].Covers != nil || got.Modules[1].ID != "B" {
		t.Fatalf("modules: %+v", got.Modules)
	}
	if !strings.Contains(readFile(t, root, ".arch/registry.yaml"), "# keep files short") {
		t.Fatal("comment lost")
	}
}

func TestModuleRm(t *testing.T) {
	root := moduleRepo(t, moduleRegistry, "src/a.go", "addons/Kit/k.gd")
	if code, out, errs := runArc("module", "set", "Kit", "--summary", "A kit.", "--path", "addons/Kit/**", "--entry", "Kit"); code != exitOK {
		t.Fatalf("set: %d %q %q", code, out, errs)
	}
	code, out, _ := runArc("module", "rm", "Kit", "--json")
	if code != exitOK || !strings.Contains(out, `"removed": "Kit"`) || !strings.Contains(out, `"existed": true`) {
		t.Fatalf("rm: %d %q", code, out)
	}
	if strings.Contains(readFile(t, root, ".arch/registry.yaml"), "Kit") {
		t.Fatal("module still in registry")
	}
	before := readFile(t, root, ".arch/registry.yaml")
	code, out, _ = runArc("module", "rm", "Kit", "--json")
	if code != exitOK || !strings.Contains(out, `"existed": false`) || readFile(t, root, ".arch/registry.yaml") != before {
		t.Fatalf("rm absent: %d %q", code, out)
	}
}

func TestModuleSetBadGlobRestoresFile(t *testing.T) {
	root := moduleRepo(t, moduleRegistry, "src/a.go")
	before := readFile(t, root, ".arch/registry.yaml")
	code, out, _ := runArc("module", "set", "Kit", "--summary", "A kit.", "--path", "addons/Nope/**", "--entry", "Kit", "--json")
	if code != exitViolations || !strings.Contains(out, `"problems"`) || !strings.Contains(out, "addons/Nope") {
		t.Fatalf("code %d out %q", code, out)
	}
	if readFile(t, root, ".arch/registry.yaml") != before {
		t.Fatal("registry changed after failed set")
	}
}

func TestModuleSetCreatesModulesKey(t *testing.T) {
	root := moduleRepo(t, moduleRegistry, "src/a.go")
	if code, out, errs := runArc("module", "set", "cobra", "--summary", "CLI.", "--import", "github.com/spf13/cobra", "--entry", "cobra.Command"); code != exitOK {
		t.Fatalf("code %d %q %q", code, out, errs)
	}
	got := readFile(t, root, ".arch/registry.yaml")
	if !strings.Contains(got, "\nmodules:\n  - id: cobra\n") || !strings.HasPrefix(got, "# Registry for the demo project.") {
		t.Fatalf("registry:\n%s", got)
	}
}

// render does not list modules (SPEC.md is silent), so the proof that set
// renders is that a missing ARCHITECTURE.md block appears.
func TestModuleSetRendersArchitecture(t *testing.T) {
	root := moduleRepo(t, moduleRegistry, "src/a.go", "addons/Kit/k.gd")
	if _, err := os.Stat(filepath.Join(root, "ARCHITECTURE.md")); err == nil {
		t.Fatal("fixture already has ARCHITECTURE.md")
	}
	code, out, errs := runArc("module", "set", "Kit", "--summary", "A kit.", "--path", "addons/Kit/**", "--entry", "Kit")
	if code != exitOK || !strings.Contains(out, "render: updated ARCHITECTURE.md") {
		t.Fatalf("code %d %q %q", code, out, errs)
	}
	if !strings.Contains(readFile(t, root, "ARCHITECTURE.md"), "<!-- arc:begin -->") {
		t.Fatal("ARCHITECTURE.md has no rendered block")
	}
	if code, out, _ := runArc("render"); code != exitOK || !strings.Contains(out, "up to date") {
		t.Fatalf("render after set: %d %q", code, out)
	}
}

func TestModuleSetUsage(t *testing.T) {
	moduleRepo(t, moduleRegistry, "src/a.go")
	for _, args := range [][]string{
		{"module"}, {"module", "bogus"}, {"module", "set", "Kit"}, {"module", "set", "--summary", "s"},
		{"module", "rm"}, {"module", "list", "x"},
	} {
		if code, _, _ := runArc(args...); code != exitUsage {
			t.Errorf("%v: code %d, want %d", args, code, exitUsage)
		}
	}
}

func TestModuleListJSONGolden(t *testing.T) {
	reg := moduleRegistry + `modules:
  - id: GodotKit
    summary: Reusable Godot UI widgets.
    paths: ["addons/GodotKit/**"]
    covers: [ui widgets, menus]
    entry: [GodotKit, addons/GodotKit/api.gd]
    rules: [Use the public API; never edit files under addons/GodotKit.]
    source: dir:../kits/GodotKit
  - id: cobra
    summary: CLI framework for Go.
    covers: [command line parsing]
    entry: [cobra.Command]
    import: github.com/spf13/cobra
`
	moduleRepo(t, reg, "src/a.go", "addons/GodotKit/api.gd")
	code, out, _ := runArc("module", "list", "--json")
	if code != exitOK {
		t.Fatalf("code %d", code)
	}
	golden(t, "module-list.json", out)
}

func TestModuleListEmpty(t *testing.T) {
	moduleRepo(t, moduleRegistry, "src/a.go")
	code, out, _ := runArc("module", "list", "--json")
	if code != exitOK || strings.TrimSpace(out) != "{\n  \"modules\": []\n}" {
		t.Fatalf("%d %q", code, out)
	}
}

// nonModulesLines returns the registry's lines outside the top-level
// modules block (its key line and the indented or blank-free lines under it).
func nonModulesLines(text string) []string {
	var keep []string
	in := false
	for _, l := range strings.Split(text, "\n") {
		switch {
		case strings.HasPrefix(l, "modules:"):
			in = true
		case in && (strings.HasPrefix(l, " ") || strings.HasPrefix(l, "\t")):
		default:
			in = false
			keep = append(keep, l)
		}
	}
	return keep
}

const fourSpaceRegistry = `# Registry, four-space style.

version: 1


system:
    name: demo   # odd spacing
    summary: Demo.

# the concepts
concepts:
    - id: game
      name: Game
      summary: The game.
      paths: ["src/**"]

modules:
    # first kit
    - id: A
      summary: A.
      entry: [A]
    # between entries
    - id: B
      summary: B.
      entry: [B]
# about limits
limits:
    file_lines: 1000

# end of file
`

func TestModuleEditsLeaveOtherBytesAlone(t *testing.T) {
	root := moduleRepo(t, fourSpaceRegistry, "src/a.go")
	before := nonModulesLines(readFile(t, root, ".arch/registry.yaml"))
	for _, args := range [][]string{
		{"module", "set", "A", "--summary", "A2.", "--entry", "A"},
		{"module", "set", "C", "--summary", "C.", "--entry", "C"},
		{"module", "rm", "B"},
		{"module", "rm", "A"},
		{"module", "rm", "C"},
	} {
		if code, out, errs := runArc(args...); code != exitOK {
			t.Fatalf("%v: %d %q %q", args, code, out, errs)
		}
		got := readFile(t, root, ".arch/registry.yaml")
		if strings.Join(nonModulesLines(got), "\n") != strings.Join(before, "\n") {
			t.Fatalf("%v changed bytes outside modules:\n%s", args, got)
		}
	}
}

func TestModuleSetKeepsCommentBetweenEntries(t *testing.T) {
	root := moduleRepo(t, fourSpaceRegistry, "src/a.go")
	if code, out, errs := runArc("module", "set", "A", "--summary", "A2.", "--entry", "A"); code != exitOK {
		t.Fatalf("%d %q %q", code, out, errs)
	}
	got := readFile(t, root, ".arch/registry.yaml")
	if !strings.Contains(got, "# between entries") || !strings.Contains(got, "# about limits\nlimits:") || !strings.Contains(got, "A2.") {
		t.Fatalf("registry:\n%s", got)
	}
}

func TestModuleSetAddsNewlineAtEOF(t *testing.T) {
	root := moduleRepo(t, "version: 1\nsystem: {name: demo}\nconcepts:\n  - {id: g, paths: [\"**\"]}", "a.go")
	if code, out, errs := runArc("module", "set", "K", "--summary", "K.", "--entry", "K"); code != exitOK {
		t.Fatalf("%d %q %q", code, out, errs)
	}
	got := readFile(t, root, ".arch/registry.yaml")
	if !strings.Contains(got, "[\"**\"]}\nmodules:\n") {
		t.Fatalf("registry:\n%s", got)
	}
}

func TestModuleRmRestoresFileOnInvalid(t *testing.T) {
	// The module hides the overlap of a and b; removing it exposes it.
	reg := `version: 1
system: {name: demo}
concepts:
  - {id: a, paths: ["lib/**"]}
  - {id: b, paths: ["lib/kit/**"]}
modules:
  - id: Kit
    summary: Kit.
    paths: ["lib/kit/**"]
    entry: [Kit]
`
	root := moduleRepo(t, reg, "lib/a.go", "lib/kit/k.go")
	code, out, _ := runArc("module", "rm", "Kit", "--json")
	if code != exitViolations || !strings.Contains(out, `"problems"`) {
		t.Fatalf("code %d out %q", code, out)
	}
	if got := readFile(t, root, ".arch/registry.yaml"); got != reg {
		t.Fatalf("registry not restored:\n%s", got)
	}
}

func TestModuleSetKeepsFileMode(t *testing.T) {
	root := moduleRepo(t, moduleRegistry, "src/a.go")
	p := filepath.Join(root, ".arch", "registry.yaml")
	if err := os.Chmod(p, 0o600); err != nil {
		t.Fatal(err)
	}
	if code, out, errs := runArc("module", "set", "K", "--summary", "K.", "--entry", "K"); code != exitOK {
		t.Fatalf("%d %q %q", code, out, errs)
	}
	fi, err := os.Stat(p)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v err %v", fi.Mode(), err)
	}
	if left, _ := filepath.Glob(filepath.Join(root, ".arch", ".registry-*")); len(left) > 0 {
		t.Fatalf("temp files left: %v", left)
	}
}

func TestModuleSetWritesThroughSymlink(t *testing.T) {
	root := moduleRepo(t, moduleRegistry, "src/a.go")
	p := filepath.Join(root, ".arch", "registry.yaml")
	real := filepath.Join(root, "real-registry.yaml")
	if err := os.Rename(p, real); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..", "real-registry.yaml"), p); err != nil {
		t.Fatal(err)
	}
	if code, out, errs := runArc("module", "set", "K", "--summary", "K.", "--entry", "K"); code != exitOK {
		t.Fatalf("%d %q %q", code, out, errs)
	}
	if fi, err := os.Lstat(p); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("registry is no longer a symlink: %v %v", fi, err)
	}
	if !strings.Contains(readFile(t, root, "real-registry.yaml"), "id: K") {
		t.Fatal("target file not updated")
	}
}
