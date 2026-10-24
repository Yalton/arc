package hook

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// codexRun feeds a fixture (with {{ROOT}} replaced) to the Codex adapter.
func codexRun(t *testing.T, root, fixture string) (resp claudeResponse, raw, stderr string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "hooks", "codex", fixture))
	if err != nil {
		t.Fatal(err)
	}
	payload := strings.ReplaceAll(string(data), "{{ROOT}}", root)
	var out, errb bytes.Buffer
	if code := Codex(strings.NewReader(payload), &out, &errb, now); code != 0 {
		t.Fatalf("exit code %d", code)
	}
	if out.Len() > 0 {
		if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
			t.Fatalf("response is not JSON: %q", out.String())
		}
	}
	return resp, out.String(), errb.String()
}

func TestCodexPatchAddingUnownedFileDenied(t *testing.T) {
	resp, _, _ := codexRun(t, newRepo(t), "pre-patch-unowned.json")
	h := resp.HookSpecificOutput
	if h == nil || h.HookEventName != "PreToolUse" || h.PermissionDecision != "deny" ||
		!strings.Contains(h.PermissionDecisionReason, "internal/foo/x.go: unowned") {
		t.Fatalf("got %+v", h)
	}
}

func TestCodexOwnedPatchContextOnce(t *testing.T) {
	root := newRepo(t)
	resp, _, _ := codexRun(t, root, "pre-patch-owned.json")
	h := resp.HookSpecificOutput
	if h == nil || h.PermissionDecision != "" || !strings.Contains(h.AdditionalContext, "Goals are work nodes.") {
		t.Fatalf("want context without a permission decision, got %+v", h)
	}
	if _, raw, _ := codexRun(t, root, "pre-patch-owned.json"); raw != "" {
		t.Fatalf("second patch in same session must be silent, got %q", raw)
	}
}

func TestCodexMalformedPayloadAllowed(t *testing.T) {
	_, raw, stderr := codexRun(t, newRepo(t), "malformed.json")
	if raw != "" || !strings.Contains(stderr, "malformed payload") {
		t.Fatalf("stdout %q stderr %q", raw, stderr)
	}
}

func TestCodexShellEvents(t *testing.T) {
	root := newRepo(t)
	if _, raw, _ := codexRun(t, root, "pre-bash.json"); raw != "" {
		t.Fatalf("pre-shell must be silent, got %q", raw)
	}
	if _, raw, _ := codexRun(t, root, "post-bash-bd-create.json"); raw != "" {
		t.Fatalf("bd create has no triage yet, got %q", raw)
	}
	ev, event, _ := CodexEvent([]byte(`{"hook_event_name":"PostToolUse","tool_name":"Bash","tool_input":{"command":"bd create x"}}`))
	if event != "PostToolUse" || ev.Kind != PostShell || ev.Command != "bd create x" {
		t.Fatalf("event %+v", ev)
	}
}

func TestCodexUnknownToolAllowed(t *testing.T) {
	ev, _, err := CodexEvent([]byte(`{"hook_event_name":"PreToolUse","tool_name":"mcp__fs__write","tool_input":{"path":"x"}}`))
	if err != nil || ev.Kind != Other {
		t.Fatalf("event %+v err %v", ev, err)
	}
}

func TestPatchPaths(t *testing.T) {
	patch := "apply_patch <<'EOF'\r\n*** Begin Patch\r\n*** Add File: a/new.go\r\n+x\r\n" +
		"*** Update File: b.go\r\n*** Move to: c/moved.go\r\n@@\r\n-y\r\n+z\r\n" +
		"*** Delete File: d.go\r\n*** Update File: a/new.go\r\n*** End Patch\r\nEOF"
	want := []string{"a/new.go", "b.go", "c/moved.go", "d.go"}
	if got := patchPaths(patch); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q", got)
	}
	ev, _, _ := CodexEvent([]byte(`{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":` +
		mustJSON(patch) + `}}`))
	if ev.Kind != PreWrite || !reflect.DeepEqual(ev.Paths, want) {
		t.Fatalf("shell-form apply_patch: %+v", ev)
	}
}

func TestCodexMoveToUnownedDenied(t *testing.T) {
	root := newRepo(t)
	patch := "*** Begin Patch\n*** Update File: legacy/old.go\n*** Move to: scratch/old.go\n@@\n-a\n+b\n*** End Patch"
	ev, _, _ := CodexEvent([]byte(`{"session_id":"s","cwd":` + mustJSON(root) +
		`,"hook_event_name":"PreToolUse","tool_name":"apply_patch","tool_input":{"command":` + mustJSON(patch) + `}}`))
	if d := Decide(ev, now); !d.Deny || !strings.Contains(d.Reason, "scratch/old.go") {
		t.Fatalf("move to a new unowned path must be denied: %+v", d)
	}
}

func mustJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
