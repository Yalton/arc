package registry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var fixtureFiles = []string{
	"docs/readme.md",
	"internal/api/server.go",
	"internal/queue/queue.go",
	"internal/scheduler/run.go",
	"internal/store/db.go",
	"internal/workgraph/goal.go",
	"internal/workgraph/goal_test.go",
	"main.go",
}

func loadFixture(t *testing.T, name string) *Registry {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "registry", name))
	if err != nil {
		t.Fatal(err)
	}
	r, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestValidFixture(t *testing.T) {
	r := loadFixture(t, "valid.yaml")
	if ps := r.Validate(fixtureFiles); len(ps) != 0 {
		t.Fatalf("unexpected problems: %v", ps)
	}
}

func TestValidationRules(t *testing.T) {
	cases := []struct {
		fixture string
		want    []string // substrings, each must appear in some problem
	}{
		{"duplicate-ids.yaml", []string{`concepts[goal]: duplicate id "goal"`}},
		{"unknown-concept.yaml", []string{
			`flags[USE_X].concept: unknown concept "nope"`,
			`removals[old].replaced_by: unknown concept "missing"`,
		}},
		{"glob-no-match.yaml", []string{
			`concepts[goal].paths: glob "internal/nothere/**" matches no files`,
			`boundaries[b].deny: glob "internal/absent/**" matches no files`,
		}},
		{"overlap.yaml", []string{
			"concepts[internal].paths: internal/workgraph/goal.go is owned by both internal and goal (2 files overlap)",
		}},
		{"bad-date.yaml", []string{
			`flags[USE_X].expires: date "12/01/2026" is not YYYY-MM-DD`,
			`removals[old].remove_by: date "soon" is not YYYY-MM-DD`,
		}},
	}
	for _, c := range cases {
		t.Run(c.fixture, func(t *testing.T) {
			ps := loadFixture(t, c.fixture).Validate(fixtureFiles)
			var all []string
			for _, p := range ps {
				all = append(all, p.String())
			}
			joined := strings.Join(all, "\n")
			for _, w := range c.want {
				if !strings.Contains(joined, w) {
					t.Errorf("missing %q in:\n%s", w, joined)
				}
			}
		})
	}
}

func TestUnknownKeyRejected(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "registry", "unknown-key.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(data); err == nil || !strings.Contains(err.Error(), "field path not found") {
		t.Fatalf("want unknown field error, got %v", err)
	}
}

func TestRemovalPathsExemptFromExistence(t *testing.T) {
	// valid.yaml has removal path internal/gone/** that matches nothing.
	for _, p := range loadFixture(t, "valid.yaml").Validate(fixtureFiles) {
		if strings.HasPrefix(p.Entry, "removals") {
			t.Fatalf("removal paths must not need to exist: %v", p)
		}
	}
}

func TestOwnerAndIgnored(t *testing.T) {
	r := loadFixture(t, "valid.yaml")
	if c := r.Owner("internal/workgraph/goal.go"); c == nil || c.ID != "goal" {
		t.Errorf("owner = %v", c)
	}
	if c := r.Owner("main.go"); c != nil {
		t.Errorf("main.go owner = %v, want nil", c.ID)
	}
	if !r.IsIgnored("internal/workgraph/goal_test.go") || !r.IsIgnored("docs/readme.md") {
		t.Error("expected ignored")
	}
	if r.IsIgnored("main.go") {
		t.Error("main.go not ignored")
	}
}

func TestLoadMissing(t *testing.T) {
	if _, err := Load(t.TempDir()); err != ErrNotFound {
		t.Fatalf("err = %v", err)
	}
}

func TestRepoFilesWalkOutsideGit(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "a", "b"), 0o755)
	os.WriteFile(filepath.Join(dir, "a", "b", "c.go"), nil, 0o644)
	os.WriteFile(filepath.Join(dir, "top.go"), nil, 0o644)
	files, err := RepoFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(files, ",") != "a/b/c.go,top.go" {
		t.Fatalf("files = %v", files)
	}
}

func TestBuiltinIgnore(t *testing.T) {
	r := &Registry{}
	for _, p := range []string{
		".arch/registry.yaml", "ARCHITECTURE.md", "AGENTS.md", "CLAUDE.md",
		".claude/settings.json", ".codex/hooks.json", ".opencode/plugins/arc.js",
	} {
		if !r.IsIgnored(p) {
			t.Errorf("%s not ignored", p)
		}
	}
	if r.IsIgnored("sub/AGENTS.md") {
		t.Error("only root AGENTS.md is built-in ignored")
	}
}
