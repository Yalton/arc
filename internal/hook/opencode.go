package hook

// opencode plugin contract, pinned for opencode v2.1.4 (plugin API v2,
// @opencode/plugin 2.0.32). Sources: the official docs
// (https://opencode.ai/v2/docs/plugins, .../v2/docs/build/plugins and
// .../build/plugins/migrate-v1), the ToolHooks types in @opencode/plugin
// dist/promise/tool.d.ts and its adapter.js, and events recorded from real
// 'opencode run' sessions (testdata/hooks/opencode/ holds them as the shim
// sends them, paths anonymised).
//
//   - Location: every .ts/.js file directly in <repo>/.opencode/plugins/
//     (plural) loads automatically; ~/.config/opencode/plugins/ globally.
//   - Module: a default export {id, setup(ctx)} (what Plugin.define
//     returns; a plain object works, so the shim needs no npm package).
//     V1 plugins (function exports returning "tool.execute.before" /
//     "tool.execute.after" hooks) do not run in v2 and are not supported.
//   - Hooks: ctx.tool.hook("execute.before", fn) and
//     ctx.tool.hook("execute.after", fn). Each gets one mutable event:
//     {tool, sessionID, agent, messageID, id (call id), input} and, after,
//     status "completed" with result {output, content, metadata} or status
//     "error" with error. ctx.location.directory is the project directory.
//   - Tools: "write" input {path, content}; "edit" input {path, oldString,
//     newString}; paths relative to the directory or absolute. Shell is
//     tool "shell" with input {command} (optionally workdir); its result
//     .output is {exit, output, truncated, status} with output the combined
//     text, and .content the text the model sees. The patch tool ("patch",
//     formerly "apply_patch") takes input.patchText in the "*** Add File:"
//     format; it was not observed in a recorded run, so its name and input
//     are taken from the binary and the v1 tool. v1 names (bash, filePath)
//     are accepted too.
//   - Blocking: throwing from execute.before rejects that call alone with
//     the error message as the tool error the model sees ("Only
//     execute.before may reject a call", adapter.js). execute.after cannot
//     reject.
//   - Context: there is no context field on tool hooks. The shim appends
//     arc's context as a text part to event.result.content in execute.after,
//     so the model reads it with the tool result.
//   - Like other harnesses, a file written by a shell command is not seen
//     as a write before it happens; the execute.after shell hook reports
//     it afterwards, appended as context (there is no feedback channel).
//
// Shim <-> arc protocol (adapters/opencode/arc.js). The shim runs 'arc
// hook opencode' with one JSON object on stdin:
//
//	{"hook": "execute.before" | "execute.after", "tool": "<name>",
//	 "session_id": "...", "call_id": "...", "directory": "<project dir>",
//	 "input": {<tool input as opencode passed it>},
//	 "result": {<tool result; execute.after only>}}
//
// arc prints nothing to allow silently, or one JSON object:
//
//	{"action": "deny", "reason": "..."}   (execute.before only)
//	{"action": "allow", "context": "..."}
//
// The shim allows on no reply, unparsable output, a missing arc or a 10s
// timeout. arc always exits 0.

import (
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"time"
)

type opencodeRequest struct {
	Hook      string `json:"hook"`
	Tool      string `json:"tool"`
	SessionID string `json:"session_id"`
	Directory string `json:"directory"`
	Input     struct {
		Path      string `json:"path"`
		FilePath  string `json:"filePath"` // v1 name
		PatchText string `json:"patchText"`
		Command   string `json:"command"`
		Workdir   string `json:"workdir"`
	} `json:"input"`
	Result struct {
		Output  json.RawMessage `json:"output"`
		Content json.RawMessage `json:"content"`
	} `json:"result"`
}

type opencodeReply struct {
	Action  string `json:"action"`
	Reason  string `json:"reason,omitempty"`
	Context string `json:"context,omitempty"`
}

// opencodeShellOutput returns a shell result's text: result.output.output
// (combined stdout and stderr), else the text parts of result.content.
func opencodeShellOutput(output, content json.RawMessage) string {
	var o struct {
		Output string `json:"output"`
	}
	if json.Unmarshal(output, &o) == nil && o.Output != "" {
		return o.Output
	}
	var s string
	if json.Unmarshal(content, &s) == nil {
		return s
	}
	var parts []struct{ Type, Text string }
	json.Unmarshal(content, &parts)
	var texts []string
	for _, p := range parts {
		if p.Type == "text" {
			texts = append(texts, p.Text)
		}
	}
	return strings.Join(texts, "\n")
}

// OpencodeEvent converts a shim request to an Event, also returning the
// hook name.
func OpencodeEvent(payload []byte) (Event, string, error) {
	var r opencodeRequest
	if err := json.Unmarshal(payload, &r); err != nil {
		return Event{}, "", err
	}
	ev := Event{SessionID: r.SessionID, Cwd: r.Directory}
	switch r.Hook {
	case "execute.before":
		switch r.Tool {
		case "write", "edit", "multiedit":
			p := r.Input.Path
			if p == "" {
				p = r.Input.FilePath
			}
			ev.Kind, ev.Paths = PreWrite, nonEmpty(p)
		case "patch", "apply_patch":
			ev.Kind, ev.Paths = PreWrite, patchPaths(r.Input.PatchText)
		}
	case "execute.after":
		if r.Tool == "shell" || r.Tool == "bash" {
			ev.Kind, ev.Command = PostShell, r.Input.Command
			ev.Output = opencodeShellOutput(r.Result.Output, r.Result.Content)
			if w := r.Input.Workdir; w != "" {
				if !filepath.IsAbs(w) {
					w = filepath.Join(r.Directory, w)
				}
				ev.Cwd = w
			}
		}
	}
	return ev, r.Hook, nil
}

// Opencode reads a shim request from stdin and writes arc's reply to
// stdout. Warnings go to stderr. It always returns exit code 0: arc's own
// failures must never block the agent.
func Opencode(stdin io.Reader, stdout, stderr io.Writer, now time.Time) int {
	data, err := io.ReadAll(stdin)
	if err != nil {
		io.WriteString(stderr, "arc hook: reading payload: "+err.Error()+"\n")
		return 0
	}
	ev, hookName, err := OpencodeEvent(data)
	if err != nil {
		io.WriteString(stderr, "arc hook: malformed payload: "+err.Error()+"\n")
		return 0
	}
	d := Decide(ev, now)
	if d.Warning != "" {
		io.WriteString(stderr, d.Warning+"\n")
	}
	var r opencodeReply
	ctx := joinContext(d.Feedback, d.Context) // execute.after can only add text
	switch {
	case d.Deny && hookName == "execute.before":
		r = opencodeReply{Action: "deny", Reason: d.Reason}
	case ctx != "":
		r = opencodeReply{Action: "allow", Context: ctx}
	default:
		return 0
	}
	json.NewEncoder(stdout).Encode(r)
	return 0
}
