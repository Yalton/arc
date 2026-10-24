package hook

// Codex CLI hook contract, pinned for codex-cli 0.164.0 ('codex features
// list' reports hooks stable). Sources: the official hooks docs
// (https://developers.openai.com/codex/hooks, now served at
// https://learn.chatgpt.com/docs/hooks) and payloads recorded from a real
// 'codex exec' run (testdata/hooks/codex/ holds them, paths anonymised).
//
//   - Config: hooks.json next to each config layer (~/.codex/hooks.json,
//     <repo>/.codex/hooks.json) or inline [hooks] tables in config.toml.
//     Every layer's hooks load. The JSON shape matches Claude Code:
//     {"hooks": {"PreToolUse": [{"matcher": "<regex on tool name>",
//     "hooks": [{"type": "command", "command": "..."}]}]}}.
//     Repo-local hooks load only when the project is trusted, and every
//     non-managed hook must be trusted by hash ('/hooks' in the TUI, or
//     'codex exec --dangerously-bypass-hook-trust') before it runs.
//   - Commands run with the session cwd; one JSON object on stdin.
//   - Payload: session_id, turn_id, transcript_path, cwd, hook_event_name,
//     model, permission_mode, tool_name, tool_use_id, tool_input, and for
//     PostToolUse tool_response (a string).
//   - Shell (shell and exec_command) arrives as tool_name "Bash" with
//     tool_input.command a string. File edits arrive as tool_name
//     "apply_patch" with tool_input.command the whole patch text; one patch
//     may touch several files via "*** Add File: p", "*** Update File: p"
//     (optionally followed by "*** Move to: q"), "*** Delete File: p".
//     Paths are relative to cwd or absolute.
//   - Response: same as Claude Code. Deny with {"hookSpecificOutput":
//     {"hookEventName":"PreToolUse","permissionDecision":"deny",
//     "permissionDecisionReason":"..."}}; add context with
//     "additionalContext" (PreToolUse or PostToolUse). Plain stdout is
//     ignored. Exit 0 without output allows; exit 2 blocks with stderr as
//     the reason. Outputs such as permissionDecision "ask"/"allow" without
//     updatedInput, continue:false or suppressOutput are unsupported: the
//     hook run is marked failed and the tool proceeds.
//   - Hooks are a guardrail, not an enforcement boundary: a file written
//     by a shell command (echo > f) is not seen as a write.

import (
	"encoding/json"
	"io"
	"strings"
	"time"
)

type codexPayload struct {
	SessionID     string `json:"session_id"`
	Cwd           string `json:"cwd"`
	HookEventName string `json:"hook_event_name"`
	ToolName      string `json:"tool_name"`
	ToolInput     struct {
		Command string `json:"command"` // Bash command line or apply_patch text
	} `json:"tool_input"`
}

// patchHeaders are the apply_patch lines that name a target path.
var patchHeaders = []string{"*** Add File:", "*** Update File:", "*** Delete File:", "*** Move to:"}

// patchPaths returns every path an apply_patch text touches, in order.
func patchPaths(patch string) []string {
	var paths []string
	for _, line := range strings.Split(patch, "\n") {
		line = strings.TrimSpace(line)
		for _, h := range patchHeaders {
			if p, ok := strings.CutPrefix(line, h); ok {
				if p = strings.TrimSpace(p); p != "" {
					paths = appendUnique(paths, p)
				}
			}
		}
	}
	return paths
}

// CodexEvent converts a Codex hook payload to an Event, also returning the
// hook event name.
func CodexEvent(payload []byte) (Event, string, error) {
	var p codexPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return Event{}, "", err
	}
	ev := Event{SessionID: p.SessionID, Cwd: p.Cwd}
	switch p.HookEventName {
	case "PreToolUse":
		switch {
		case p.ToolName == "apply_patch":
			ev.Kind, ev.Paths = PreWrite, patchPaths(p.ToolInput.Command)
		case p.ToolName == "Bash" && strings.Contains(p.ToolInput.Command, "*** Begin Patch"):
			// apply_patch invoked through the shell (heredoc form).
			ev.Kind, ev.Paths = PreWrite, patchPaths(p.ToolInput.Command)
		}
	case "PostToolUse":
		if p.ToolName == "Bash" {
			ev.Kind, ev.Command = PostShell, p.ToolInput.Command
		}
	}
	return ev, p.HookEventName, nil
}

// Codex reads a Codex hook payload from stdin and writes the response to
// stdout. Warnings go to stderr. It always returns exit code 0: denials are
// expressed in the JSON response, and arc's own failures must never block
// the agent.
func Codex(stdin io.Reader, stdout, stderr io.Writer, now time.Time) int {
	data, err := io.ReadAll(stdin)
	if err != nil {
		io.WriteString(stderr, "arc hook: reading payload: "+err.Error()+"\n")
		return 0
	}
	ev, event, err := CodexEvent(data)
	if err != nil {
		io.WriteString(stderr, "arc hook: malformed payload: "+err.Error()+"\n")
		return 0
	}
	d := Decide(ev, now)
	if d.Warning != "" {
		io.WriteString(stderr, d.Warning+"\n")
	}
	out := &claudeHookOutput{HookEventName: event} // Codex uses the same shape
	switch {
	case d.Deny && event == "PreToolUse":
		out.PermissionDecision = "deny"
		out.PermissionDecisionReason = d.Reason
	case d.Context != "":
		out.AdditionalContext = d.Context
	default:
		return 0
	}
	json.NewEncoder(stdout).Encode(claudeResponse{HookSpecificOutput: out})
	return 0
}
