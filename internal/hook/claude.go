package hook

import (
	"encoding/json"
	"io"
	"time"
)

// Claude Code hook payloads (PreToolUse / PostToolUse command hooks).
// Only the fields arc reads are declared.
type claudePayload struct {
	SessionID     string          `json:"session_id"`
	Cwd           string          `json:"cwd"`
	HookEventName string          `json:"hook_event_name"`
	ToolName      string          `json:"tool_name"`
	ToolInput     claudeToolInput `json:"tool_input"`
	ToolResponse  json.RawMessage `json:"tool_response"` // PostToolUse only
}

// claudeBashResponse is the Bash tool_response of a PostToolUse payload.
type claudeBashResponse struct {
	Stdout string `json:"stdout"`
}

type claudeToolInput struct {
	FilePath     string `json:"file_path"`     // Write, Edit, MultiEdit
	NotebookPath string `json:"notebook_path"` // NotebookEdit
	Command      string `json:"command"`       // Bash
}

type claudeResponse struct {
	HookSpecificOutput *claudeHookOutput `json:"hookSpecificOutput,omitempty"`
	SystemMessage      string            `json:"systemMessage,omitempty"`
}

type claudeHookOutput struct {
	HookEventName            string `json:"hookEventName"`
	PermissionDecision       string `json:"permissionDecision,omitempty"`
	PermissionDecisionReason string `json:"permissionDecisionReason,omitempty"`
	AdditionalContext        string `json:"additionalContext,omitempty"`
}

// ClaudeEvent converts a Claude Code hook payload to an Event.
func ClaudeEvent(payload []byte) (Event, string, error) {
	var p claudePayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return Event{}, "", err
	}
	ev := Event{SessionID: p.SessionID, Cwd: p.Cwd}
	switch p.HookEventName {
	case "PreToolUse":
		switch p.ToolName {
		case "Write", "Edit", "MultiEdit":
			ev.Kind, ev.Paths = PreWrite, nonEmpty(p.ToolInput.FilePath)
		case "NotebookEdit":
			ev.Kind, ev.Paths = PreWrite, nonEmpty(p.ToolInput.NotebookPath)
		}
	case "PostToolUse":
		if p.ToolName == "Bash" {
			ev.Kind, ev.Command = PostShell, p.ToolInput.Command
			var r claudeBashResponse
			if json.Unmarshal(p.ToolResponse, &r) == nil { // a non-object response just means no output
				ev.Output = r.Stdout
			}
		}
	}
	return ev, p.HookEventName, nil
}

func nonEmpty(s string) []string {
	if s == "" {
		return nil
	}
	return []string{s}
}

// Claude reads a Claude Code hook payload from stdin and writes the
// response to stdout. Warnings go to stderr. It always returns exit code 0:
// denials are expressed in the JSON response, and arc's own failures must
// never block the agent.
func Claude(stdin io.Reader, stdout, stderr io.Writer, now time.Time) int {
	data, err := io.ReadAll(stdin)
	if err != nil {
		io.WriteString(stderr, "arc hook: reading payload: "+err.Error()+"\n")
		return 0
	}
	ev, event, err := ClaudeEvent(data)
	if err != nil {
		io.WriteString(stderr, "arc hook: malformed payload: "+err.Error()+"\n")
		return 0
	}
	d := Decide(ev, now)
	if d.Warning != "" {
		io.WriteString(stderr, d.Warning+"\n")
	}
	out := &claudeHookOutput{HookEventName: event}
	switch {
	case d.Deny:
		out.PermissionDecision = "deny"
		out.PermissionDecisionReason = d.Reason
	case d.Context != "":
		out.AdditionalContext = d.Context
	default:
		return 0 // allow without output; the normal permission flow applies
	}
	json.NewEncoder(stdout).Encode(claudeResponse{HookSpecificOutput: out})
	return 0
}
