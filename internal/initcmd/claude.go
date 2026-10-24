package initcmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// claudeSettingsPath is the project-shared Claude Code settings file.
const claudeSettingsPath = ".claude/settings.json"

// ClaudeHookCommand is the command Claude Code runs for arc's hooks.
const ClaudeHookCommand = "arc hook claude"

// claudeHooks are the hook entries arc needs, per event: matcher -> command.
var claudeHooks = []struct{ event, matcher string }{
	{"PreToolUse", "Write|Edit|MultiEdit|NotebookEdit"},
	{"PostToolUse", "Bash"},
}

// ensureClaude merges arc's hook entries into .claude/settings.json,
// keeping every existing setting and hook. Entries already running
// ClaudeHookCommand for an event are left alone. Re-encoding sorts keys.
func ensureClaude(root string, res *Result) error {
	path := filepath.Join(root, filepath.FromSlash(claudeSettingsPath))
	data, err := os.ReadFile(path)
	exists := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	settings := map[string]any{}
	if exists && len(bytes.TrimSpace(data)) > 0 {
		if err := json.Unmarshal(data, &settings); err != nil {
			return fmt.Errorf("%s: %w (fix it, then rerun)", claudeSettingsPath, err)
		}
	}
	hooks, ok := settings["hooks"].(map[string]any)
	if settings["hooks"] != nil && !ok {
		return fmt.Errorf("%s: hooks is not an object", claudeSettingsPath)
	}
	if hooks == nil {
		hooks = map[string]any{}
	}
	changed := false
	for _, h := range claudeHooks {
		groups, ok := hooks[h.event].([]any)
		if hooks[h.event] != nil && !ok {
			return fmt.Errorf("%s: hooks.%s is not a list", claudeSettingsPath, h.event)
		}
		if hasCommand(groups, ClaudeHookCommand) {
			continue
		}
		hooks[h.event] = append(groups, map[string]any{
			"matcher": h.matcher,
			"hooks":   []any{map[string]any{"type": "command", "command": ClaudeHookCommand}},
		})
		changed = true
	}
	if !changed {
		return nil
	}
	settings["hooks"] = hooks
	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, append(out, '\n'), 0o644); err != nil {
		return err
	}
	if exists {
		res.updated(claudeSettingsPath)
	} else {
		res.created(claudeSettingsPath)
	}
	return nil
}

// hasCommand reports whether any hook in the matcher groups runs cmd.
func hasCommand(groups []any, cmd string) bool {
	for _, g := range groups {
		gm, _ := g.(map[string]any)
		hs, _ := gm["hooks"].([]any)
		for _, h := range hs {
			hm, _ := h.(map[string]any)
			if hm["command"] == cmd {
				return true
			}
		}
	}
	return false
}
