package initcmd

// claudeSettingsPath is the project-shared Claude Code settings file.
const claudeSettingsPath = ".claude/settings.json"

// ClaudeHookCommand is the command Claude Code runs for arc's hooks.
const ClaudeHookCommand = "arc hook claude"

// claudeHooks are the hook entries arc needs, per event: matcher -> command.
var claudeHooks = []hookEntry{
	{"PreToolUse", "Write|Edit|MultiEdit|NotebookEdit"},
	{"PostToolUse", "Bash"},
}

// ensureClaude merges arc's hook entries into .claude/settings.json,
// keeping every existing setting and hook. Entries already running
// ClaudeHookCommand for an event are left alone. Re-encoding sorts keys.
func ensureClaude(root string, res *Result) error {
	return mergeHooksJSON(root, claudeSettingsPath, ClaudeHookCommand, claudeHooks, res)
}
