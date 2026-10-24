package initcmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureClaudeMergesWithoutClobbering(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".claude", "settings.json")
	os.MkdirAll(filepath.Dir(path), 0o755)
	existing := `{"permissions": {"allow": ["Bash(ls)"]},
 "hooks": {"PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "mine"}]}]}}`
	os.WriteFile(path, []byte(existing), 0o644)

	res := &Result{}
	if err := ensureClaude(root, res); err != nil {
		t.Fatal(err)
	}
	if len(res.Updated) != 1 {
		t.Fatalf("updated = %v", res.Updated)
	}
	data, _ := os.ReadFile(path)
	var s struct {
		Permissions map[string][]string
		Hooks       map[string][]struct {
			Matcher string
			Hooks   []struct{ Type, Command string }
		}
	}
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	if s.Permissions["allow"][0] != "Bash(ls)" {
		t.Error("permissions lost")
	}
	pre := s.Hooks["PreToolUse"]
	if len(pre) != 2 || pre[0].Hooks[0].Command != "mine" || pre[1].Hooks[0].Command != ClaudeHookCommand ||
		!strings.Contains(pre[1].Matcher, "Write") {
		t.Fatalf("PreToolUse = %+v", pre)
	}
	if post := s.Hooks["PostToolUse"]; len(post) != 1 || post[0].Matcher != "Bash" {
		t.Fatalf("PostToolUse = %+v", post)
	}

	res = &Result{}
	if err := ensureClaude(root, res); err != nil || len(res.Updated)+len(res.Created) != 0 {
		t.Fatalf("rerun changed %v %v, err %v", res.Created, res.Updated, err)
	}
	again, _ := os.ReadFile(path)
	if string(again) != string(data) {
		t.Fatal("rerun rewrote the file")
	}
}

func TestEnsureClaudeCreatesAndRejectsBadJSON(t *testing.T) {
	root := t.TempDir()
	res := &Result{}
	if err := ensureClaude(root, res); err != nil || len(res.Created) != 1 {
		t.Fatalf("created %v err %v", res.Created, err)
	}
	os.WriteFile(filepath.Join(root, ".claude", "settings.json"), []byte("{oops"), 0o644)
	if err := ensureClaude(root, &Result{}); err == nil {
		t.Fatal("malformed settings must be an error, not overwritten")
	}
}

func TestGlobEscape(t *testing.T) {
	if got := globEscape("[locale]{x}*"); got != `\[locale\]\{x\}\*` {
		t.Fatalf("got %q", got)
	}
}
