package symbols

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Yalton/arc/internal/registry"
)

var update = flag.Bool("update", false, "rewrite golden files in testdata/symbols")

const fixtureDir = "../../testdata/symbols"

func golden(t *testing.T, name, got string) {
	t.Helper()
	p := filepath.Join(fixtureDir, name)
	if *update {
		if err := os.WriteFile(p, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("%v (run go test ./internal/symbols/ -update)", err)
	}
	if got != string(want) {
		t.Errorf("%s mismatch\n--- got ---\n%s--- want ---\n%s", name, got, want)
	}
}

var fixtureFiles = []string{
	"go/lib.go", "go/lib_test.go", "python/mod.py", "python/test_mod.py",
	"ts/mod.ts", "ts/mod.test.ts", "missing.go",
}

func TestSymbolsGolden(t *testing.T) {
	got, err := Symbols(fixtureDir, fixtureFiles)
	if err != nil {
		t.Fatal(err)
	}
	for _, skipped := range []string{"go/lib_test.go", "python/test_mod.py", "ts/mod.test.ts", "missing.go"} {
		if _, ok := got[skipped]; ok {
			t.Errorf("%s must be skipped", skipped)
		}
	}
	js, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "symbols.json", string(js)+"\n")
}

func TestByConcept(t *testing.T) {
	reg, err := registry.Parse([]byte(`version: 1
ignore: ["ts/**"]
concepts:
  - {id: lib, paths: ["go/**"]}
  - {id: py, paths: ["python/**"]}
  - {id: web, paths: ["ts/**"]}
`))
	if err != nil {
		t.Fatal(err)
	}
	got, err := ByConcept(reg, fixtureDir, fixtureFiles, map[string]bool{"lib": true, "web": true})
	if err != nil {
		t.Fatal(err)
	}
	if len(got["lib"]) == 0 || len(got["py"]) != 0 || len(got["web"]) != 0 {
		t.Fatalf("want only lib (py not asked for, web ignored): %v", got)
	}
}

func TestFirstSentence(t *testing.T) {
	for in, want := range map[string]string{
		"Add appends.\nMore here.":    "Add appends.",
		"No period at all":            "No period at all",
		"v1.2 is a version. Next.":    "v1.2 is a version.",
		"Renders, e.g. a line. More.": "Renders, e.g. a line.",
	} {
		if got := firstSentence(in); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
	long := firstSentence("word " + strings.Repeat("x", 200))
	if len([]rune(long)) != maxDoc {
		t.Errorf("long doc is %d runes, want %d", len([]rune(long)), maxDoc)
	}
}
