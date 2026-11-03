package audit

import (
	"slices"
	"testing"

	"github.com/Yalton/arc/internal/registry"
)

func TestModulePathsAreNotUnowned(t *testing.T) {
	reg, err := registry.Parse([]byte(`version: 1
system: {name: demo}
concepts:
  - {id: alpha, paths: ["internal/a/**"]}
modules:
  - {id: Kit, paths: ["addons/Kit/**"], entry: [Kit]}
`))
	if err != nil {
		t.Fatal(err)
	}
	rep := &Report{}
	rep.addRegistry(Options{Reg: reg, Files: []string{"internal/a/x.go", "addons/Kit/a.cs", "stray.go"}, Now: testNow}, testNow)
	if !slices.Equal(rep.Unowned, []string{"stray.go"}) {
		t.Fatalf("Unowned = %v, want only stray.go", rep.Unowned)
	}
}
