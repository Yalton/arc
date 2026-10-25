package symbols

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Yalton/arc/internal/registry"
)

func TestTokens(t *testing.T) {
	for in, want := range map[string]string{
		"parseHTTPLine":           "pars http lin",
		"parse_http_line":         "pars http lin",
		"Parsing the parsed line": "pars pars lin",
		"labels Labelled mapped":  "label labell map",
		"entries classes matches": "entry class match",
		"status analysis v2Store": "status analysis v2 stor",
		"How do I add a bead?":    "add bead",
	} {
		if got := strings.Join(Tokens(in), " "); got != want {
			t.Errorf("Tokens(%q) = %q, want %q", in, got, want)
		}
	}
}

const findRegistry = `version: 1
concepts:
  - id: beads
    name: Beads integration
    summary: bd CLI wrapper and bead triage.
    paths: ["internal/beads/**"]
    aliases: [bead, label]
  - id: imports
    name: Import extraction
    summary: Imports of source files.
    paths: ["internal/imports/**"]
    rules: [Python and TS parse import lines.]
`

var findProvides = map[string][]Symbol{
	"beads": {
		{Name: "Client.AddLabel", Kind: KindMethod, Doc: "AddLabel adds a label to a bead.", File: "internal/beads/beads.go", Line: 10},
		{Name: "Client.Show", Kind: KindMethod, Doc: "Show reads one bead.", File: "internal/beads/beads.go", Line: 20},
	},
	"imports": {
		{Name: "ParseImportLine", Kind: KindFunc, Doc: "ParseImportLine parses one import line.", File: "internal/imports/python.go", Line: 5},
		{Name: "Imports", Kind: KindFunc, Doc: "Imports reads files.", File: "internal/imports/imports.go", Line: 3},
	},
}

func findReg(t *testing.T) *registry.Registry {
	t.Helper()
	reg, err := registry.Parse([]byte(findRegistry))
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

func TestFindRanks(t *testing.T) {
	res := Find(findReg(t), findProvides, "parsing import lines")
	if !reflect.DeepEqual(res.Query, []string{"parsing", "import", "lines"}) {
		t.Errorf("query %v", res.Query)
	}
	if len(res.Concepts) != 1 || res.Concepts[0].ID != "imports" ||
		!reflect.DeepEqual(res.Concepts[0].Why, []string{"parsing (rules)", "import (id)", "lines (rules)"}) {
		t.Errorf("concepts %+v", res.Concepts)
	}
	if len(res.Symbols) == 0 || res.Symbols[0].Name != "ParseImportLine" || res.Symbols[0].Concept != "imports" {
		t.Errorf("symbols %+v", res.Symbols)
	}

	res = Find(findReg(t), findProvides, "add a bead label")
	if res.Concepts[0].ID != "beads" || res.Symbols[0].Name != "Client.AddLabel" {
		t.Errorf("got %+v", res)
	}
	if res := Find(findReg(t), findProvides, "kubernetes operator"); len(res.Concepts)+len(res.Symbols) != 0 {
		t.Errorf("want no match, got %+v", res)
	}
	if res := Find(findReg(t), findProvides, "the of"); len(res.Query) != 0 {
		t.Errorf("stopwords only: %+v", res)
	}
}

func TestCovered(t *testing.T) {
	reg := findReg(t)
	hits := Covered(reg, findProvides, "Add a label to beads when triage passes", 3)
	if len(hits) != 1 || hits[0].Name != "Client.AddLabel" {
		t.Fatalf("got %+v", hits)
	}
	// One-word names (Show, Imports) are never strong evidence on their own.
	if hits := Covered(reg, findProvides, "show imports", 3); len(hits) != 0 {
		t.Fatalf("got %+v", hits)
	}
}
