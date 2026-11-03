package brief

import (
	"testing"

	"github.com/Yalton/arc/internal/registry"
)

func TestPlaceModule(t *testing.T) {
	reg, err := registry.Parse([]byte(`version: 1
system: {name: demo}
ignore: ["addons/**"]
concepts:
  - {id: game, paths: ["**"]}
modules:
  - {id: Kit, paths: ["addons/Kit/**"], entry: [Kit]}
`))
	if err != nil {
		t.Fatal(err)
	}
	r := Place(reg, "addons/Kit/a.cs")
	if r.Status != StatusModule || r.Module != "Kit" || r.Concept != "" {
		t.Fatalf("got %+v", r) // module wins over ignore and ownership
	}
	if got, want := PlaceMessage(r), "addons/Kit/a.cs: module Kit (installed; use, never edit)"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if r := Place(reg, "Player.cs"); r.Status != StatusOwned || r.Concept != "game" {
		t.Fatalf("got %+v", r)
	}
}
