package initcmd

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Markers delimiting arc's section of a pre-commit hook. Text outside them
// belongs to the user or to other tools (e.g. beads) and is never touched.
const (
	blockBegin = "# --- BEGIN ARC ---"
	blockEnd   = "# --- END ARC ---"
)

// legacyHookMarker identifies the whole-file hook older arc versions
// installed (with any original hook moved to pre-commit.local).
const legacyHookMarker = "# arc pre-commit hook"

// hookBlock is arc's section of the pre-commit hook. It is a valid script
// body on its own and safe to append to any hook that falls through: it runs
// in a subshell so no variables leak, skips silently outside an arc
// repository (e.g. a shared core.hooksPath), warns and skips when arc is not
// on PATH, and exits the hook only when the gate fails.
const hookBlock = blockBegin + `
# Managed by 'arc init'; edits between these markers are overwritten on rerun.
(
	top="$(git rev-parse --show-toplevel 2>/dev/null)" || exit 0
	[ -f "$top/.arch/registry.yaml" ] || exit 0
	if ! command -v arc >/dev/null 2>&1; then
		echo "arc pre-commit: 'arc' not found on PATH; skipping architecture gate" >&2
		exit 0
	fi
	exec arc gate --staged
) || exit $?
` + blockEnd + "\n"

// freshHook is installed when there is no pre-commit hook.
const freshHook = "#!/bin/sh\n" + hookBlock

// chainHook is installed when an existing hook cannot take an appended block
// (see needsChain); the original lives in pre-commit.local and runs first.
const chainHook = `#!/bin/sh
# Installed by 'arc init': your original hook is pre-commit.local next to
# this file; it runs first and its failure blocks the commit.
local_hook="$(dirname "$0")/pre-commit.local"
if [ -x "$local_hook" ]; then
	"$local_hook" "$@" || exit $?
fi
` + hookBlock

// hooksDir resolves the hooks directory for the repository at top, honouring
// core.hooksPath and linked worktrees.
func hooksDir(top string) (string, error) {
	cmd := exec.Command("git", "-C", top, "rev-parse", "--git-path", "hooks")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse --git-path hooks: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	dir := filepath.FromSlash(strings.TrimRight(string(out), "\r\n"))
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(top, dir)
	}
	return filepath.Clean(dir), nil
}

// ensureHook installs or refreshes arc's block in the pre-commit hook:
//
//   - no hook: write a fresh one;
//   - hook with the arc block: replace the block in place if stale;
//   - legacy whole-file arc hook: restore pre-commit.local (if any) and treat
//     it as a foreign hook, else write a fresh one;
//   - foreign hook: append the block, unless needsChain says the block could
//     never run, in which case move it to pre-commit.local and chain it.
func ensureHook(root string, res *Result) error {
	top := gitTop(root)
	if top == "" {
		res.notice("not a git repository; pre-commit hook not installed")
		return nil
	}
	dir, err := hooksDir(top)
	if err != nil {
		return err
	}
	hook := filepath.Join(dir, "pre-commit")
	local := filepath.Join(dir, "pre-commit.local")
	report := func(p string) string { return relOrAbs(top, p) }

	fi, err := os.Lstat(hook)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		if err := writeHook(hook, freshHook, 0o755); err != nil {
			return err
		}
		res.created(report(hook))
		return nil
	}
	if err != nil {
		return err
	}
	var data []byte
	if fi.Mode().IsRegular() {
		if data, err = os.ReadFile(hook); err != nil {
			return err
		}
	}

	switch {
	case bytes.Contains(data, []byte(blockBegin)):
		updated, err := replaceBlock(data)
		if err != nil {
			return fmt.Errorf("%s: %w", report(hook), err)
		}
		if bytes.Equal(updated, data) {
			return nil
		}
		if err := writeHook(hook, string(updated), fi.Mode().Perm()); err != nil {
			return err
		}
		res.updated(report(hook))
		return nil

	case bytes.Contains(data, []byte(legacyHookMarker)):
		lfi, err := os.Lstat(local)
		if errors.Is(err, os.ErrNotExist) {
			if err := writeHook(hook, freshHook, 0o755); err != nil {
				return err
			}
			res.updated(report(hook))
			res.notice("replaced the old arc pre-commit hook with the marker-block layout")
			return nil
		}
		if err != nil {
			return err
		}
		ldata, _ := os.ReadFile(local)
		if needsChain(lfi, ldata) {
			if err := writeHook(hook, chainHook, 0o755); err != nil {
				return err
			}
			res.updated(report(hook))
			res.notice("refreshed the arc pre-commit hook; %s still runs first", report(local))
			return nil
		}
		if err := os.Rename(local, hook); err != nil {
			return err
		}
		if err := writeHook(hook, appendBlock(ldata), lfi.Mode().Perm()); err != nil {
			return err
		}
		res.updated(report(hook))
		res.notice("moved %s back to %s and appended the arc block", report(local), report(hook))
		return nil

	case needsChain(fi, data):
		if _, err := os.Lstat(local); err == nil {
			return fmt.Errorf("%s exists and is not arc's, but %s is taken; merge them by hand", report(hook), report(local))
		}
		if err := os.Rename(hook, local); err != nil {
			return err
		}
		if err := writeHook(hook, chainHook, 0o755); err != nil {
			return err
		}
		res.updated(report(hook))
		res.notice("existing pre-commit hook moved to %s and chained before arc gate", report(local))
		return nil

	default:
		if err := writeHook(hook, appendBlock(data), fi.Mode().Perm()); err != nil {
			return err
		}
		res.updated(report(hook))
		res.notice("appended the arc block to the existing pre-commit hook %s", report(hook))
		return nil
	}
}

// needsChain reports whether an existing hook must be moved aside and chained
// rather than have the arc block appended:
//
//   - it is a symlink (appending would edit the shared target) or not a
//     regular file;
//   - it is not executable (git never runs it, and appending would activate
//     its dormant content);
//   - its last meaningful line ends the script unconditionally.
//
// The last check is a heuristic: the last line that is neither blank nor a
// comment counts as terminal when it starts at column 0 (so it is not inside
// an indented if/case/function body) with the word "exec" or "exit". A
// terminal command nested without indentation is missed, and an indented one
// at top level is treated as conditional.
func needsChain(fi os.FileInfo, data []byte) bool {
	if !fi.Mode().IsRegular() || fi.Mode().Perm()&0o111 == 0 {
		return true
	}
	return endsInExecOrExit(data)
}

func endsInExecOrExit(data []byte) bool {
	lines := strings.Split(string(data), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimRight(lines[i], " \t\r")
		trimmed := strings.TrimLeft(line, " \t")
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if line != trimmed {
			return false // indented: assume inside a conditional
		}
		word, _, _ := strings.Cut(line, " ")
		word, _, _ = strings.Cut(word, "\t")
		word = strings.TrimRight(word, ";")
		return word == "exec" || word == "exit"
	}
	return false
}

// replaceBlock swaps the text from the begin marker line through the end
// marker line for the current block.
func replaceBlock(data []byte) ([]byte, error) {
	s := string(data)
	b := strings.Index(s, blockBegin)
	if b > 0 && s[b-1] != '\n' {
		return nil, fmt.Errorf("arc begin marker is not at the start of a line; fix it by hand")
	}
	rel := strings.Index(s[b:], blockEnd)
	if rel < 0 {
		return nil, fmt.Errorf("%q without %q; fix it by hand", blockBegin, blockEnd)
	}
	e := b + rel + len(blockEnd)
	if nl := strings.IndexByte(s[e:], '\n'); nl >= 0 {
		e += nl + 1
	} else {
		e = len(s)
	}
	return []byte(s[:b] + hookBlock + s[e:]), nil
}

// appendBlock adds the block at the end of a hook, separated by a blank line.
func appendBlock(data []byte) string {
	s := string(data)
	if s != "" && !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	if s != "" {
		s += "\n"
	}
	return s + hookBlock
}

// writeHook writes content to p and makes it executable; perm supplies the
// remaining bits so an existing hook keeps its mode.
func writeHook(p, content string, perm os.FileMode) error {
	if err := os.WriteFile(p, []byte(content), perm|0o755); err != nil {
		return err
	}
	return os.Chmod(p, perm|0o755) // WriteFile's mode is masked by umask
}
