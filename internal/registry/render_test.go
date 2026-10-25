package registry

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files")

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", "render", name)
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("%s mismatch (run with -update to accept):\n%s", name, got)
	}
}

func TestRenderArchitectureGolden(t *testing.T) {
	golden(t, "architecture-block.md", loadFixture(t, "valid.yaml").RenderArchitecture())
}

func TestRenderFreshRepo(t *testing.T) {
	root := t.TempDir()
	r := loadFixture(t, "valid.yaml")
	changed, err := r.Render(root)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(changed, ",") != "ARCHITECTURE.md,AGENTS.md" {
		t.Fatalf("changed = %v (CLAUDE.md must only be touched when present)", changed)
	}
	data, _ := os.ReadFile(filepath.Join(root, "ARCHITECTURE.md"))
	golden(t, "ARCHITECTURE.md", string(data))
	data, _ = os.ReadFile(filepath.Join(root, "AGENTS.md"))
	golden(t, "AGENTS.md", string(data))

	again, err := r.Render(root)
	if err != nil || len(again) != 0 {
		t.Fatalf("second render changed %v, err %v", again, err)
	}
	if stale, _ := r.IsStale(root); stale {
		t.Fatal("stale right after render")
	}
}

func TestRenderPreservesOutsideText(t *testing.T) {
	root := t.TempDir()
	before := "# Claude\n\nKeep me.\n\n" + BeginMarker + "\nOLD-CONTENT\n" + EndMarker + "\n\nAnd me.\n"
	os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte(before), 0o644)
	if _, err := loadFixture(t, "valid.yaml").Render(root); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	got := string(data)
	if !strings.HasPrefix(got, "# Claude\n\nKeep me.\n\n"+BeginMarker+"\n") ||
		!strings.HasSuffix(got, EndMarker+"\n\nAnd me.\n") || strings.Contains(got, "OLD-CONTENT") {
		t.Fatalf("outside text not preserved:\n%s", got)
	}
	if n := strings.Count(Snippet(true), "\n"); n >= 15 {
		t.Fatalf("snippet has %d lines, want < 15", n)
	}
}

func TestStaleAfterRegistryChange(t *testing.T) {
	root := t.TempDir()
	r := loadFixture(t, "valid.yaml")
	if stale, _ := r.IsStale(root); !stale {
		t.Fatal("missing ARCHITECTURE.md must be stale")
	}
	r.Render(root)
	r.Concepts[0].Summary = "Changed."
	s, err := r.Stale(root)
	if err != nil || strings.Join(s, ",") != "ARCHITECTURE.md" {
		t.Fatalf("stale = %v, err %v", s, err)
	}
}

func TestRenderBeadsLine(t *testing.T) {
	root := t.TempDir()
	r := loadFixture(t, "valid.yaml")
	if _, err := r.Render(root); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if strings.Contains(string(data), "Concepts touched") {
		t.Fatalf("bead line without beads: true:\n%s", data)
	}
	// An untracked .beads/ workspace must not change the output: a fresh
	// clone (CI) has none, and render must agree with it.
	os.Mkdir(filepath.Join(root, ".beads"), 0o755)
	if stale, _ := r.IsStale(root); stale {
		t.Fatal(".beads/ must not change the snippet; only the registry field does")
	}
	r.Beads = true
	if s, _ := r.Stale(root); strings.Join(s, ",") != "AGENTS.md" {
		t.Fatalf("stale = %v, want AGENTS.md once the registry sets beads: true", s)
	}
	if _, err := r.Render(root); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(filepath.Join(root, "AGENTS.md"))
	golden(t, "AGENTS-beads.md", string(data))
	os.Remove(filepath.Join(root, ".beads"))
	if stale, _ := r.IsStale(root); stale {
		t.Fatal("removing .beads/ made the beads snippet stale")
	}
}

func TestReplaceBlockMalformed(t *testing.T) {
	for _, doc := range []string{
		EndMarker + "\n" + BeginMarker,
		BeginMarker + " only",
		EndMarker + " only",
		BeginMarker + EndMarker + BeginMarker + EndMarker,
	} {
		if _, err := ReplaceBlock(doc, "x\n"); err == nil {
			t.Errorf("no error for %q", doc)
		}
	}
}
