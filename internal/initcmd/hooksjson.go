package initcmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// hookEntry is one hook arc needs: event -> matcher group running a command.
type hookEntry struct{ event, matcher string }

// mergeHooksJSON merges arc's hook entries into the JSON hooks file at rel
// (Claude Code settings.json and Codex hooks.json share the shape
// {"hooks": {"<Event>": [{"matcher": "...", "hooks": [{"type": "command",
// "command": "..."}]}]}}), keeping every existing key and hook. Events that
// already run cmd are left alone. Re-encoding sorts keys.
func mergeHooksJSON(root, rel, cmd string, entries []hookEntry, res *Result) error {
	path := filepath.Join(root, filepath.FromSlash(rel))
	data, err := os.ReadFile(path)
	exists := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	settings := map[string]any{}
	if exists && len(bytes.TrimSpace(data)) > 0 {
		if err := json.Unmarshal(data, &settings); err != nil {
			return fmt.Errorf("%s: %w (fix it, then rerun)", rel, err)
		}
	}
	hooks, ok := settings["hooks"].(map[string]any)
	if settings["hooks"] != nil && !ok {
		return fmt.Errorf("%s: hooks is not an object", rel)
	}
	if hooks == nil {
		hooks = map[string]any{}
	}
	changed := false
	for _, h := range entries {
		groups, ok := hooks[h.event].([]any)
		if hooks[h.event] != nil && !ok {
			return fmt.Errorf("%s: hooks.%s is not a list", rel, h.event)
		}
		if hasCommand(groups, cmd) {
			continue
		}
		hooks[h.event] = append(groups, map[string]any{
			"matcher": h.matcher,
			"hooks":   []any{map[string]any{"type": "command", "command": cmd}},
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
		res.updated(rel)
	} else {
		res.created(rel)
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
