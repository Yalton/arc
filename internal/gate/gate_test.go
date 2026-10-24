package gate

import (
	"strings"
	"testing"
)

const baseRegistry = `version: 1
system: {name: demo, summary: Demo.}
ignore: ["**/*_test.go", "*.md", ".arch/**"]
concepts:
  - id: goal
    name: Goal
    summary: Goals.
    paths: ["internal/goal/**"]
`

func setup(t *testing.T) *testRepo {
	r := newRepo(t, baseRegistry)
	r.write("internal/goal/goal.go", "package goal\n")
	r.write("legacy/old.go", "package legacy\n") // pre-existing unowned
	r.render()
	r.commitAll("init")
	return r
}

func TestPlacementUnownedAddedFileFails(t *testing.T) {
	r := setup(t)
	r.write("internal/foo/x.go", "package foo\n")
	r.git("add", "-A")
	rep := r.gate("staged", "placement")
	got := findings(rep)
	if rep.Errors != 1 || !strings.Contains(got, "internal/foo/x.go: unowned.") ||
		!strings.Contains(got, "goal") {
		t.Fatalf("errors %d:\n%s", rep.Errors, got)
	}
}

func TestPlacementOwnedAndIgnoredPass(t *testing.T) {
	r := setup(t)
	r.write("internal/goal/more.go", "package goal\n")
	r.write("internal/foo/x_test.go", "package foo\n")
	r.git("add", "-A")
	if rep := r.gate("staged", "placement"); rep.Errors != 0 {
		t.Fatalf("unexpected:\n%s", findings(rep))
	}
}

// Adapter files installed by init pass placement even when the registry
// predates the skeleton's ignore entries for them.
func TestPlacementAdapterDirsBuiltinIgnored(t *testing.T) {
	r := setup(t)
	r.write(".opencode/plugins/arc.js", "export default {}\n")
	r.write(".claude/settings.json", "{}\n")
	r.write(".codex/hooks.json", "{}\n")
	r.git("add", "-A")
	if rep := r.gate("staged", "placement"); rep.Errors != 0 {
		t.Fatalf("unexpected:\n%s", findings(rep))
	}
}

func TestPlacementPreExistingOnlyFailsWithAll(t *testing.T) {
	r := setup(t)
	r.write("internal/goal/goal.go", "package goal\n\nvar X = 1\n")
	r.git("add", "-A")
	if rep := r.gate("staged", "placement"); rep.Errors != 0 {
		t.Fatalf("staged should pass:\n%s", findings(rep))
	}
	rep := r.gate("all", "placement")
	if rep.Errors != 1 || !strings.Contains(findings(rep), "legacy/old.go") {
		t.Fatalf("--all should flag legacy/old.go:\n%s", findings(rep))
	}
}

func TestPlacementBaseMode(t *testing.T) {
	r := setup(t)
	r.git("checkout", "-q", "-b", "feature")
	r.write("web/app.go", "package web\n")
	r.commitAll("add web")
	rep := r.gate("main", "placement")
	if rep.Errors != 1 || !strings.Contains(findings(rep), "web/app.go") {
		t.Fatalf("got:\n%s", findings(rep))
	}
}

func TestRegistryCheck(t *testing.T) {
	r := setup(t)
	if rep := r.gate("all", "registry"); rep.Errors != 0 {
		t.Fatalf("fresh render should pass:\n%s", findings(rep))
	}
	r.write(".arch/registry.yaml", strings.Replace(baseRegistry, "summary: Goals.", "summary: Changed.", 1))
	rep := r.gate("all", "registry")
	if rep.Errors != 1 || !strings.Contains(findings(rep), "ARCHITECTURE.md: arc block is stale") {
		t.Fatalf("stale render should fail:\n%s", findings(rep))
	}
	r.write(".arch/registry.yaml", baseRegistry+`  - id: ghost
    paths: ["nowhere/**"]
`)
	r.render()
	if !strings.Contains(findings(r.gate("all", "registry")), `glob "nowhere/**" matches no files`) {
		t.Fatal("validation problem not reported")
	}
}

func TestDraftsWarnOnly(t *testing.T) {
	r := newRepo(t, strings.Replace(baseRegistry, "summary: Goals.", "summary: Goals.\n    draft: true", 1))
	r.write("internal/goal/goal.go", "package goal\n")
	r.render()
	rep := r.gate("all", "drafts")
	if rep.Errors != 0 || rep.Warnings != 1 || !strings.Contains(findings(rep), "draft concepts: goal") {
		t.Fatalf("got errors %d warnings %d:\n%s", rep.Errors, rep.Warnings, findings(rep))
	}
}

func TestUnknownOnly(t *testing.T) {
	if _, err := Run(&Context{}, []string{"nope"}); err == nil {
		t.Fatal("want error")
	}
}
