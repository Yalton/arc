package initcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Yalton/arc/internal/gate"
	"github.com/Yalton/arc/internal/registry"
)

var duplCheck = []registry.Check{cloneChecks[gate.DuplID]}

func TestAppendChecksEdits(t *testing.T) {
	item := "  - id: dupl\n    command: [\"dupl\", \"-plumbing\", \".\"]\n    baseline: true\n"
	cases := []struct{ name, in, want string }{
		{"missing key",
			"version: 1 # keep\n",
			"version: 1 # keep\n\n# External tools run by 'arc gate'.\nchecks:\n" + item},
		{"empty flow list",
			"version: 1\n# tools\nchecks: [] # none yet\nlimits:\n  file_lines: 10\n",
			"version: 1\n# tools\nchecks: # none yet\n" + item + "limits:\n  file_lines: 10\n"},
		{"block list then comment and key",
			"version: 1\nchecks:\n  - id: lint # mine\n    command: [lint]\n\n# limits below\nlimits:\n  file_lines: 10\n",
			"version: 1\nchecks:\n  - id: lint # mine\n    command: [lint]\n" + item + "\n# limits below\nlimits:\n  file_lines: 10\n"},
		{"block list at EOF without newline",
			"version: 1\nchecks:\n- id: lint\n  command: [lint]",
			"version: 1\nchecks:\n- id: lint\n  command: [lint]\n" + strings.ReplaceAll(item, "\n  ", "\n")[2:]},
	}
	for _, c := range cases {
		got, ok := appendChecks(c.in, duplCheck)
		if !ok || got != c.want {
			t.Errorf("%s: ok %v\ngot:\n%s\nwant:\n%s", c.name, ok, got, c.want)
		}
	}
	for _, bad := range []string{
		"version: 1\nchecks: [{id: lint, command: [lint]}]\n",
		"version: 1\nchecks: {}\n",
		"- not a mapping\n",
	} {
		if _, ok := appendChecks(bad, duplCheck); ok {
			t.Errorf("unsafe edit accepted for %q", bad)
		}
	}
}

// fakeDupl puts a dupl on PATH that prints one clone.
func fakeDupl(t *testing.T) {
	t.Helper()
	bin := t.TempDir()
	script := "#!/bin/sh\necho 'a/a.go:1-9: duplicate of b/b.go:3-11'\n"
	if err := os.WriteFile(filepath.Join(bin, "dupl"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestInitAddsDuplWithBaseline(t *testing.T) {
	fakeDupl(t)
	root := newRepo(t, "a/a.go", "b/b.go")
	res, err := Run(root, Options{Dupl: true})
	if err != nil {
		t.Fatal(err)
	}
	if !contains(res.Created, registry.Path) || contains(res.Updated, registry.Path) || !contains(res.Created, ".arch/baseline/dupl.txt") {
		t.Fatalf("created %v updated %v", res.Created, res.Updated)
	}
	if !anyContains(res.Notices, "checks.dupl: 1 existing finding(s) accepted") {
		t.Errorf("notices = %v", res.Notices)
	}
	reg := validate(t, root)
	if k := gate.ExternalCheck(reg, "dupl"); k == nil || !k.Baseline || strings.Join(k.Command, " ") != "dupl -plumbing ." {
		t.Fatalf("checks = %+v", reg.Checks)
	}
	if got := readFile(t, filepath.Join(root, ".arch", "baseline", "dupl.txt")); got != "a/a.go: duplicate of b/b.go\n" {
		t.Errorf("baseline = %q", got)
	}
	if !strings.Contains(readFile(t, filepath.Join(root, registry.Path)), "# External tools run by 'arc gate'.") {
		t.Error("skeleton comments lost")
	}

	// Rerun: the id is present, so nothing is added or re-baselined.
	res, err = Run(root, Options{Dupl: true})
	if err != nil || len(res.Created)+len(res.Updated) != 0 {
		t.Fatalf("rerun: %v created %v updated %v", err, res.Created, res.Updated)
	}
}

func TestInitAddsDuplToExistingRegistry(t *testing.T) {
	fakeDupl(t)
	root := newRepo(t, "a/a.go")
	reg := "# my registry\nversion: 1\nchecks:\n  - id: lint # keep me\n    command: [\"true\"]\n# trailing\n"
	write(t, root, registry.Path, reg)
	res, err := Run(root, Options{Dupl: true})
	if err != nil {
		t.Fatal(err)
	}
	if !contains(res.Updated, registry.Path) {
		t.Fatalf("updated = %v", res.Updated)
	}
	want := "# my registry\nversion: 1\nchecks:\n  - id: lint # keep me\n    command: [\"true\"]\n" +
		"  - id: dupl\n    command: [\"dupl\", \"-plumbing\", \".\"]\n    baseline: true\n# trailing\n"
	if got := readFile(t, filepath.Join(root, registry.Path)); got != want {
		t.Errorf("registry:\n%s", got)
	}
}

func TestInitPrintsYAMLWhenEditUnsafe(t *testing.T) {
	fakeDupl(t)
	root := newRepo(t, "a/a.go")
	reg := "version: 1\nchecks: [{id: lint, command: [\"true\"]}]\n"
	write(t, root, registry.Path, reg)
	res, err := Run(root, Options{Dupl: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(root, registry.Path)); got != reg {
		t.Errorf("registry rewritten:\n%s", got)
	}
	if !anyContains(res.Notices, "add under checks:\n  - id: dupl\n    command: [\"dupl\", \"-plumbing\", \".\"]\n    baseline: true\n") {
		t.Errorf("notices = %q", res.Notices)
	}
	if _, err := os.Stat(filepath.Join(root, ".arch", "baseline", "dupl.txt")); err == nil {
		t.Error("baseline recorded for a check not in the registry")
	}
}

func TestDetectCloneTools(t *testing.T) {
	root := newRepo(t, "main.go")
	bin := fakeBin(t, "dupl", "jscpd")
	for _, id := range []string{"dupl", "jscpd"} {
		if on, why := detectClone(root, bin, id); !on || why != id+" on PATH ("+filepath.Join(bin, id)+")" {
			t.Errorf("%s: %v %s", id, on, why)
		}
	}
	if on, why := detectClone(root, fakeBin(t), "dupl"); on || !strings.Contains(why, "go install github.com/mibk/dupl@latest") {
		t.Errorf("dupl missing: %v %s", on, why)
	}
	noGo := newRepo(t, "app.js")
	if on, why := detectClone(noGo, bin, "dupl"); on || why != "no Go files" {
		t.Errorf("dupl without Go: %v %s", on, why)
	}
	if on, _ := detectClone(noGo, bin, "jscpd"); !on {
		t.Error("jscpd needs no Go files")
	}
}
