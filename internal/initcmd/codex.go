package initcmd

// codexHooksPath is the repo-local Codex hooks file. Codex loads it only
// for a trusted project, and runs each hook only once trusted by hash (see
// the contract in internal/hook/codex.go).
const codexHooksPath = ".codex/hooks.json"

// CodexHookCommand is the command Codex runs for arc's hooks.
const CodexHookCommand = "arc hook codex"

// codexHooks are the hook entries arc needs. Codex reports file edits as
// tool_name apply_patch and shell commands as Bash.
var codexHooks = []hookEntry{
	{"PreToolUse", "^apply_patch$"},
	{"PostToolUse", "^Bash$"},
}

// ensureCodex merges arc's hook entries into .codex/hooks.json without
// touching other hooks or keys, and reminds the user that Codex must trust
// the project and the hooks before they run.
func ensureCodex(root string, res *Result) error {
	if err := mergeHooksJSON(root, codexHooksPath, CodexHookCommand, codexHooks, res); err != nil {
		return err
	}
	res.notice("Codex runs %s only in a trusted project and after each hook is trusted: "+
		"open codex here and review them with /hooks ('arc' must be on PATH)", codexHooksPath)
	return nil
}
