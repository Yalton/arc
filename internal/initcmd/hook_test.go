package initcmd

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// legacyHook is the whole-file hook arc installed before the marker block.
const legacyHook = `#!/bin/sh
# arc pre-commit hook (installed by 'arc init').
# Put your own checks in pre-commit.local next to this file; it runs first.
local_hook="$(dirname "$0")/pre-commit.local"
if [ -x "$local_hook" ]; then
	"$local_hook" "$@" || exit $?
fi
if ! command -v arc >/dev/null 2>&1; then
	echo "arc pre-commit: 'arc' not found on PATH; skipping architecture gate" >&2
	exit 0
fi
top="$(git rev-parse --show-toplevel 2>/dev/null)" || exit 0
if [ ! -f "$top/.arch/registry.yaml" ]; then
	exit 0 # not an arc repository (e.g. a shared core.hooksPath)
fi
exec arc gate --staged
`

// beadsSection is a beads-style managed section that falls through on success.
func beadsSection(version string) string {
	return "# --- BEGIN BEADS INTEGRATION " + version + " ---\n" +
		"# This section is managed by beads. Do not remove these markers.\n" +
		"if command -v bd >/dev/null 2>&1; then\n" +
		"  bd hooks run pre-commit \"$@\"\n" +
		"  _bd_exit=$?\n" +
		"  if [ \"$_bd_exit\" -ne 0 ]; then exit \"$_bd_exit\"; fi\n" +
		"fi\n" +
		"# --- END BEADS INTEGRATION " + version + " ---\n"
}

// fakeTool puts an executable name in bin that appends its name and
// arguments to log and exits with $<NAME>_EXIT (default 0).
func fakeTool(t *testing.T, bin, name, log string) {
	t.Helper()
	env := strings.ToUpper(name) + "_EXIT"
	script := "#!/bin/sh\necho \"" + name + " $*\" >> " + log + "\nexit ${" + env + ":-0}\n"
	if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

// hookEnv returns a PATH holding fake arc (and bd), plus the shared log.
func hookEnv(t *testing.T) (path, log string) {
	t.Helper()
	bin := t.TempDir()
	log = filepath.Join(bin, "calls.log")
	fakeTool(t, bin, "arc", log)
	fakeTool(t, bin, "bd", log)
	return bin + string(os.PathListSeparator) + os.Getenv("PATH"), log
}

// readLog returns the logged calls and truncates the log.
func readLog(t *testing.T, log string) string {
	t.Helper()
	data, _ := os.ReadFile(log)
	os.Remove(log)
	return string(data)
}

func runHook(t *testing.T, root, hook, path string) (int, string) {
	t.Helper()
	cmd := exec.Command("sh", hook)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "PATH="+path)
	out, err := cmd.CombinedOutput()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), string(out)
	}
	if err != nil {
		t.Fatal(err)
	}
	return 0, string(out)
}

func installHook(t *testing.T, root, rel, body string) string {
	t.Helper()
	write(t, root, rel, body)
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.Chmod(p, 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func mustRun(t *testing.T, root string) *Result {
	t.Helper()
	res, err := Run(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// assertRerunNoop reruns init and checks nothing changed.
func assertRerunNoop(t *testing.T, root string) {
	t.Helper()
	before := snapshot(t, root)
	res := mustRun(t, root)
	if len(res.Created)+len(res.Updated) != 0 {
		t.Errorf("rerun changed files: %+v", res)
	}
	if after := snapshot(t, root); !reflect.DeepEqual(before, after) {
		t.Errorf("rerun changed contents")
	}
}

func TestHookFresh(t *testing.T) {
	root := newRepo(t, "a.go")
	mustRun(t, root)
	hook := filepath.Join(root, ".git", "hooks", "pre-commit")
	if got := readFile(t, hook); got != freshHook {
		t.Fatalf("hook = %q", got)
	}
	path, log := hookEnv(t)
	if code, out := runHook(t, root, hook, path); code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if got := readLog(t, log); got != "arc gate --staged\n" {
		t.Errorf("calls = %q", got)
	}
	t.Setenv("ARC_EXIT", "1")
	if code, _ := runHook(t, root, hook, path); code != 1 {
		t.Errorf("gate failure exit = %d, want 1", code)
	}
	assertRerunNoop(t, root)
}

func TestHookAppendsToForeign(t *testing.T) {
	root := newRepo(t, "a.go")
	path, log := hookEnv(t)
	foreign := "#!/bin/sh\necho foreign >> " + log + "\n[ -z \"$FOREIGN_FAIL\" ] || exit 3"
	hook := installHook(t, root, ".git/hooks/pre-commit", foreign)
	res := mustRun(t, root)
	if !reflect.DeepEqual(res.Updated, []string{".git/hooks/pre-commit"}) {
		t.Errorf("updated = %v", res.Updated)
	}
	if got := readFile(t, hook); got != foreign+"\n\n"+hookBlock {
		t.Fatalf("hook = %q", got)
	}
	if _, err := os.Lstat(hook + ".local"); err == nil {
		t.Error("pre-commit.local created")
	}
	if fi, _ := os.Stat(hook); fi.Mode().Perm()&0o111 == 0 {
		t.Error("hook not executable")
	}

	if code, out := runHook(t, root, hook, path); code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if got := readLog(t, log); got != "foreign\narc gate --staged\n" {
		t.Errorf("calls = %q", got)
	}
	t.Setenv("FOREIGN_FAIL", "1")
	if code, _ := runHook(t, root, hook, path); code != 3 {
		t.Errorf("foreign failure exit = %d, want 3", code)
	}
	if got := readLog(t, log); got != "foreign\n" {
		t.Errorf("arc ran after foreign failure: %q", got)
	}
	assertRerunNoop(t, root)
}

func TestHookBeadsCoexist(t *testing.T) {
	root := newRepo(t, "a.go")
	git(t, root, "config", "core.hooksPath", ".beads/hooks")
	head := "#!/usr/bin/env sh\n"
	hook := installHook(t, root, ".beads/hooks/pre-commit", head+beadsSection("v1.3.0"))
	mustRun(t, root)
	path, log := hookEnv(t)
	if code, out := runHook(t, root, hook, path); code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if got := readLog(t, log); got != "bd hooks run pre-commit\narc gate --staged\n" {
		t.Errorf("calls = %q", got)
	}
	assertRerunNoop(t, root)

	// beads rewrites its own section (as 'bd hooks install' does).
	data := readFile(t, hook)
	start := strings.Index(data, "# --- BEGIN BEADS")
	endMarker := "# --- END BEADS INTEGRATION v1.3.0 ---\n"
	end := strings.Index(data, endMarker) + len(endMarker)
	rewritten := data[:start] + beadsSection("v1.4.0") + data[end:]
	if err := os.WriteFile(hook, []byte(rewritten), 0o755); err != nil {
		t.Fatal(err)
	}
	assertRerunNoop(t, root)
	if got := readFile(t, hook); strings.Count(got, blockBegin) != 1 || !strings.Contains(got, "v1.4.0") {
		t.Fatalf("hook = %q", got)
	}
	if code, out := runHook(t, root, hook, path); code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if got := readLog(t, log); got != "bd hooks run pre-commit\narc gate --staged\n" {
		t.Errorf("calls after beads rewrite = %q", got)
	}
}

func TestHookBlockRefreshedInPlace(t *testing.T) {
	root := newRepo(t, "a.go")
	before, after := "#!/bin/sh\necho before\n", "echo after\n"
	hook := installHook(t, root, ".git/hooks/pre-commit",
		before+blockBegin+"\nexec arc gate --old\n"+blockEnd+"\n"+after)
	res := mustRun(t, root)
	if !reflect.DeepEqual(res.Updated, []string{".git/hooks/pre-commit"}) {
		t.Errorf("updated = %v", res.Updated)
	}
	if got := readFile(t, hook); got != before+hookBlock+after {
		t.Fatalf("hook = %q", got)
	}
	assertRerunNoop(t, root)
}

func TestHookExitFallsBackToChain(t *testing.T) {
	root := newRepo(t, "a.go")
	path, log := hookEnv(t)
	foreign := "#!/bin/sh\necho foreign >> " + log + "\nexit ${LOCAL_EXIT:-0}\n"
	hook := installHook(t, root, ".git/hooks/pre-commit", foreign)
	res := mustRun(t, root)
	if got := readFile(t, hook); got != chainHook {
		t.Fatalf("hook = %q", got)
	}
	if got := readFile(t, hook+".local"); got != foreign {
		t.Fatalf("pre-commit.local = %q", got)
	}
	if !strings.Contains(strings.Join(res.Notices, "\n"), "chained") {
		t.Errorf("notices = %v", res.Notices)
	}
	if code, out := runHook(t, root, hook, path); code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if got := readLog(t, log); got != "foreign\narc gate --staged\n" {
		t.Errorf("calls = %q", got)
	}
	t.Setenv("LOCAL_EXIT", "3")
	if code, _ := runHook(t, root, hook, path); code != 3 {
		t.Errorf("exit = %d, want 3", code)
	}
	if got := readLog(t, log); got != "foreign\n" {
		t.Errorf("arc ran after chained hook failed: %q", got)
	}
	assertRerunNoop(t, root)
}

func TestHookMigratesLegacyLayout(t *testing.T) {
	falls := "#!/bin/sh\necho mine\n"
	exits := "#!/bin/sh\necho mine\nexit 0\n"
	cases := []struct {
		name, local         string // local "" = no pre-commit.local
		wantHook, wantLocal string // wantLocal "" = absent
	}{
		{"restores and appends", falls, falls + "\n" + hookBlock, ""},
		{"no local", "", freshHook, ""},
		{"local ends in exit", exits, chainHook, exits},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := newRepo(t, "a.go")
			hook := installHook(t, root, ".git/hooks/pre-commit", legacyHook)
			if c.local != "" {
				installHook(t, root, ".git/hooks/pre-commit.local", c.local)
			}
			res := mustRun(t, root)
			if !reflect.DeepEqual(res.Updated, []string{".git/hooks/pre-commit"}) || len(res.Notices) == 0 {
				t.Errorf("result = %+v", res)
			}
			if got := readFile(t, hook); got != c.wantHook {
				t.Errorf("hook = %q", got)
			}
			local, err := os.ReadFile(hook + ".local")
			if c.wantLocal == "" && err == nil {
				t.Error("pre-commit.local still present")
			}
			if c.wantLocal != "" && string(local) != c.wantLocal {
				t.Errorf("pre-commit.local = %q, %v", local, err)
			}
			assertRerunNoop(t, root)
		})
	}
}

func TestHookSkipsOutsideArcRepo(t *testing.T) {
	root := newRepo(t, "a.go")
	mustRun(t, root)
	if err := os.Remove(filepath.Join(root, ".arch", "registry.yaml")); err != nil {
		t.Fatal(err)
	}
	path, log := hookEnv(t)
	code, out := runHook(t, root, filepath.Join(root, ".git", "hooks", "pre-commit"), path)
	if code != 0 || out != "" {
		t.Errorf("exit %d, out %q", code, out)
	}
	if got := readLog(t, log); got != "" {
		t.Errorf("arc ran: %q", got)
	}
}

func TestHookMissingArcWarns(t *testing.T) {
	root := newRepo(t, "a.go")
	mustRun(t, root)
	bin := t.TempDir()
	gitBin, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(gitBin, filepath.Join(bin, "git")); err != nil {
		t.Fatal(err)
	}
	code, out := runHook(t, root, filepath.Join(root, ".git", "hooks", "pre-commit"), bin)
	if code != 0 || !strings.Contains(out, "not found") {
		t.Errorf("exit %d, out %q", code, out)
	}
}

func TestHooksPathRespected(t *testing.T) {
	root := newRepo(t, "a.go")
	git(t, root, "config", "core.hooksPath", ".githooks")
	res := mustRun(t, root)
	if got := readFile(t, filepath.Join(root, ".githooks", "pre-commit")); got != freshHook {
		t.Fatalf("hook in core.hooksPath = %q", got)
	}
	if _, err := os.Stat(filepath.Join(root, ".git", "hooks", "pre-commit")); err == nil {
		t.Error("hook also written to .git/hooks")
	}
	found := false
	for _, c := range res.Created {
		found = found || c == ".githooks/pre-commit"
	}
	if !found {
		t.Errorf("created = %v", res.Created)
	}
}

func TestEndsInExecOrExit(t *testing.T) {
	cases := map[string]bool{
		"#!/bin/sh\nexit 0\n":                   true,
		"#!/bin/sh\nexec foo \"$@\"\n\n# end\n": true,
		"#!/bin/sh\nexit\n":                     true,
		"#!/bin/sh\nexit;\n":                    true,
		"#!/bin/sh\nif x; then\n  exit 1\nfi\n": false,
		"#!/bin/sh\nfoo || exit 1\n":            false,
		"#!/bin/sh\n  exit 0\n":                 false,
		"#!/bin/sh\nexiting\n":                  false,
		"#!/bin/sh\n":                           false,
	}
	for in, want := range cases {
		if got := endsInExecOrExit([]byte(in)); got != want {
			t.Errorf("endsInExecOrExit(%q) = %v, want %v", in, got, want)
		}
	}
}
