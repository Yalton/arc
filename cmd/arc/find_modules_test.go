package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

const modulesFindRegistry = beadsRegistry + `modules:
  - id: kit
    summary: Mouse cursor helpers.
    paths: ["addons/Kit/**"]
    covers: [mouse cursor stack, window focus]
    entry: [CursorStack.push]
`

func TestFindModulesFirst(t *testing.T) {
	beadsRepo(t)
	if err := os.WriteFile(".arch/registry.yaml", []byte(modulesFindRegistry), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, _ := runArc("find", "mouse", "cursor", "stack")
	if code != exitOK || !strings.HasPrefix(out, "- kit [module]: Mouse cursor helpers. [") ||
		!strings.Contains(out, "use this; do not reimplement") {
		t.Fatalf("text: code %d, out:\n%s", code, out)
	}
	code, out, _ = runArc("--json", "find", "mouse", "cursor", "stack")
	var res struct {
		Modules []struct {
			ID, Kind, Note string
			Score          int
			Entry          []string
		}
	}
	if code != exitOK || json.Unmarshal([]byte(out), &res) != nil || len(res.Modules) != 1 ||
		res.Modules[0].ID != "kit" || res.Modules[0].Kind != "module" ||
		res.Modules[0].Note != "use this; do not reimplement" || res.Modules[0].Score == 0 {
		t.Fatalf("json: code %d, out %s", code, out)
	}
}
