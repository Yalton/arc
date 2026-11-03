package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

const briefModules = `modules:
  - id: CursorKit
    summary: Custom mouse cursors.
    paths: ["addons/CursorKit/**"]
    covers: [custom mouse cursor]
    entry: [CursorKit.set_cursor]
    rules: [Use set_cursor.]
  - id: retry
    summary: Retry with backoff.
    import: example.com/retry
`

func TestBriefModules(t *testing.T) {
	state := beadsRepo(t)
	if err := os.WriteFile(".arch/registry.yaml", []byte(beadsRegistry+briefModules), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, _ := runArc("brief")
	if code != exitOK || !strings.Contains(out, "Modules:\nCursorKit — Custom mouse cursors.\n  entry: CursorKit.set_cursor\n  rules: Use set_cursor.\nretry — Retry with backoff.\n  entry: import example.com/retry") {
		t.Fatalf("code %d out %q", code, out)
	}
	code, out, _ = runArc("brief", "--modules")
	if code != exitOK || strings.Contains(out, "System:") || !strings.HasPrefix(out, "Modules:\nCursorKit") || !strings.Contains(out, "retry") {
		t.Fatalf("code %d out %q", code, out)
	}
	code, out, _ = runArc("brief", "--modules", "--json")
	var js struct{ Modules []struct{ ID string } }
	if code != exitOK || json.Unmarshal([]byte(out), &js) != nil || len(js.Modules) != 2 {
		t.Fatalf("code %d out %q", code, out)
	}
	for _, args := range [][]string{{"--modules", "internal/goal/goal.go"}, {"--modules", "--concept", "goal"}, {"--modules", "--bead", "t-1"}} {
		if code, _, _ := runArc(append([]string{"brief"}, args...)...); code != exitUsage {
			t.Fatalf("%v: code %d", args, code)
		}
	}
	// A bead's text names a module's entry and covers words.
	fakeShow(t, state, "t-1", "Touches internal/goal/x.go and needs a custom mouse cursor")
	code, out, _ = runArc("brief", "--bead", "t-1")
	if code != exitOK || !strings.Contains(out, "CursorKit — Custom") || strings.Contains(out, "retry") {
		t.Fatalf("code %d out %q", code, out)
	}
	code, out, _ = runArc("brief", "addons/CursorKit/a.gd")
	if code != exitOK || !strings.Contains(out, "CursorKit — Custom") {
		t.Fatalf("code %d out %q", code, out)
	}
}
