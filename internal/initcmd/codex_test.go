package initcmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureCodexMergesWithoutClobbering(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".codex", "hooks.json")
	os.MkdirAll(filepath.Dir(path), 0o755)
	existing := `{"description": "mine",
 "hooks": {"SessionStart": [{"matcher": "startup", "hooks": [{"type": "command", "command": "bd codex-hook SessionStart"}]}],
           "PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "guard"}]}]}}`
	os.WriteFile(path, []byte(existing), 0o644)

	res := &Result{}
	if err := ensureCodex(root, res); err != nil {
		t.Fatal(err)
	}
	if len(res.Updated) != 1 || res.Updated[0] != ".codex/hooks.json" || len(res.Notices) != 1 {
		t.Fatalf("updated %v notices %v", res.Updated, res.Notices)
	}
	data, _ := os.ReadFile(path)
	var s struct {
		Description string
		Hooks       map[string][]struct {
			Matcher string
			Hooks   []struct{ Type, Command string }
		}
	}
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	if s.Description != "mine" || len(s.Hooks["SessionStart"]) != 1 {
		t.Fatalf("existing content lost: %s", data)
	}
	pre := s.Hooks["PreToolUse"]
	if len(pre) != 2 || pre[0].Hooks[0].Command != "guard" || pre[1].Matcher != "^apply_patch$" ||
		pre[1].Hooks[0].Command != CodexHookCommand || pre[1].Hooks[0].Type != "command" {
		t.Fatalf("PreToolUse = %+v", pre)
	}
	if post := s.Hooks["PostToolUse"]; len(post) != 1 || post[0].Matcher != "^Bash$" {
		t.Fatalf("PostToolUse = %+v", post)
	}

	res = &Result{}
	if err := ensureCodex(root, res); err != nil || len(res.Updated)+len(res.Created) != 0 {
		t.Fatalf("rerun changed %v %v, err %v", res.Created, res.Updated, err)
	}
	if again, _ := os.ReadFile(path); string(again) != string(data) {
		t.Fatal("rerun rewrote the file")
	}
}

func TestEnsureCodexCreatesAndRejectsBadJSON(t *testing.T) {
	root := t.TempDir()
	res := &Result{}
	if err := ensureCodex(root, res); err != nil || len(res.Created) != 1 {
		t.Fatalf("created %v err %v", res.Created, err)
	}
	bad := filepath.Join(root, ".codex", "hooks.json")
	os.WriteFile(bad, []byte("{oops"), 0o644)
	if err := ensureCodex(root, &Result{}); err == nil {
		t.Fatal("malformed hooks.json must be an error, not overwritten")
	}
	if data, _ := os.ReadFile(bad); string(data) != "{oops" {
		t.Fatal("malformed file was modified")
	}
}
