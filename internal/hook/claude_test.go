package hook

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// claudeRun feeds a fixture (with {{ROOT}} replaced) to the Claude adapter.
func claudeRun(t *testing.T, root, fixture string) (resp claudeResponse, raw, stderr string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "hooks", "claude", fixture))
	if err != nil {
		t.Fatal(err)
	}
	payload := strings.ReplaceAll(string(data), "{{ROOT}}", root)
	var out, errb bytes.Buffer
	if code := Claude(strings.NewReader(payload), &out, &errb, now); code != 0 {
		t.Fatalf("exit code %d", code)
	}
	if out.Len() > 0 {
		if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
			t.Fatalf("response is not JSON: %q", out.String())
		}
	}
	return resp, out.String(), errb.String()
}

func TestClaudeUnownedNewFileDenied(t *testing.T) {
	resp, _, _ := claudeRun(t, newRepo(t), "pre-write-unowned.json")
	h := resp.HookSpecificOutput
	if h == nil || h.HookEventName != "PreToolUse" || h.PermissionDecision != "deny" ||
		!strings.Contains(h.PermissionDecisionReason, "internal/foo/x.go: unowned") {
		t.Fatalf("got %+v", h)
	}
}

func TestClaudeOwnedFileContextOnce(t *testing.T) {
	root := newRepo(t)
	resp, _, _ := claudeRun(t, root, "pre-edit-owned.json")
	h := resp.HookSpecificOutput
	if h == nil || h.PermissionDecision != "" || !strings.Contains(h.AdditionalContext, "Goals are work nodes.") {
		t.Fatalf("want context without a permission decision, got %+v", h)
	}
	if _, raw, _ := claudeRun(t, root, "pre-edit-owned.json"); raw != "" {
		t.Fatalf("second edit in same session must be silent, got %q", raw)
	}
}

func TestClaudeMalformedPayloadAllowed(t *testing.T) {
	_, raw, stderr := claudeRun(t, newRepo(t), "malformed.json")
	if raw != "" || !strings.Contains(stderr, "malformed payload") {
		t.Fatalf("stdout %q stderr %q", raw, stderr)
	}
}

func TestClaudePostBashBdCreateTriages(t *testing.T) {
	root := newRepo(t)
	state := fakeBd(t)
	fakeShowBead(t, state, "arc-1", "Replaces / removes: none\nConcepts touched: goal\nClosest existing thing: internal/goal")
	resp, _, stderr := claudeRun(t, root, "post-bash-bd-create.json")
	h := resp.HookSpecificOutput
	if h == nil || h.HookEventName != "PostToolUse" || h.PermissionDecision != "" ||
		!strings.Contains(h.AdditionalContext, "arc-1: arch:ok (concepts: goal)") {
		t.Fatalf("got %+v stderr %q", h, stderr)
	}
	if calls := readFake(t, state, "calls"); !strings.Contains(calls, "show arc-1 --json") || !strings.Contains(calls, "label add arc-1 arch:ok") {
		t.Fatalf("calls %q", calls)
	}
}

func TestClaudeEventPostBashOutput(t *testing.T) {
	ev, _, err := ClaudeEvent([]byte(`{"hook_event_name":"PostToolUse","tool_name":"Bash","tool_input":{"command":"bd create x"},"tool_response":{"stdout":"out"}}`))
	if err != nil || ev.Kind != PostShell || ev.Command != "bd create x" || ev.Output != "out" {
		t.Fatalf("event %+v %v", ev, err)
	}
	// Other tools may answer with a non-object; that must not break parsing.
	if _, _, err := ClaudeEvent([]byte(`{"hook_event_name":"PostToolUse","tool_name":"Bash","tool_response":"text"}`)); err != nil {
		t.Fatal(err)
	}
}
