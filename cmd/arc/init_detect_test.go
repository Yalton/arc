package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// detectRepo makes a git repository with code and an origin remote, and sets
// PATH to a directory holding only git and a fake opencode.
func detectRepo(t *testing.T, origin string) string {
	t.Helper()
	gitBin, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not on PATH")
	}
	bin := t.TempDir()
	if err := os.Symlink(gitBin, filepath.Join(bin, "git")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "opencode"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	root := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"remote", "add", "origin", origin}} {
		if out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	os.MkdirAll(filepath.Join(root, "internal", "a"), 0o755)
	os.WriteFile(filepath.Join(root, "internal", "a", "a.go"), []byte("package a\n"), 0o644)
	t.Chdir(root)
	return root
}

func TestInitDetects(t *testing.T) {
	root := detectRepo(t, "https://git.example.com/x/y.git")
	code, out, errOut := runArc("init")
	if code != exitOK {
		t.Fatalf("init: code %d\nout %s\nerr %s", code, out, errOut)
	}
	for _, want := range []string{
		"not detected claude: no .claude/ and claude not on PATH\n",
		"detected opencode: opencode on PATH (",
		"detected ci gitea: origin host git.example.com is not github.com",
		"detected scan: new registry and 1 file(s) to scan\n",
		"created .opencode/plugins/arc.js\n",
		"created .gitea/workflows/arc.yml\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if !strings.HasPrefix(out, "not detected claude") {
		t.Errorf("detection not listed first:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(root, ".claude")); err == nil {
		t.Error(".claude created without detection")
	}
}

func TestInitDetectForeignWorkflowIsNotice(t *testing.T) {
	root := detectRepo(t, "git@github.com:x/y.git")
	os.MkdirAll(filepath.Join(root, ".github", "workflows"), 0o755)
	os.WriteFile(filepath.Join(root, ".github", "workflows", "arc.yml"), []byte("name: mine\n"), 0o644)
	code, out, errOut := runArc("init")
	if code != exitOK || !strings.Contains(out, "note: .github/workflows/arc.yml exists and is not managed by arc") {
		t.Fatalf("init: code %d\nout %s\nerr %s", code, out, errOut)
	}
	if code, _, errOut := runArc("init", "--ci", "github"); code != exitUsage || !strings.Contains(errOut, "not managed by arc") {
		t.Fatalf("explicit --ci github over foreign file: code %d err %s", code, errOut)
	}
}

func TestInitNoDetectAndOptOuts(t *testing.T) {
	root := detectRepo(t, "https://github.com/x/y.git")
	code, out, errOut := runArc("init", "--no-detect")
	if code != exitOK || strings.Contains(out, "detected") || strings.Contains(out, "workflows") || strings.Contains(out, ".opencode") {
		t.Fatalf("init --no-detect: code %d\nout %s\nerr %s", code, out, errOut)
	}
	code, out, errOut = runArc("init", "--json", "--ci", "none", "--opencode=false")
	if code != exitOK {
		t.Fatalf("init opt-outs: code %d err %s", code, errOut)
	}
	var res struct {
		Detected []struct{ Item, Value, Source, Reason string }
		Created  []string
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	got := map[string]string{}
	for _, f := range res.Detected {
		got[f.Item] = f.Value + " " + f.Source
	}
	if got["ci"] != "none flag" || got["opencode"] != "off flag" || got["scan"] != "off detected" || len(res.Created) != 0 {
		t.Errorf("detected %v created %v", got, res.Created)
	}
	if _, err := os.Stat(filepath.Join(root, ".github")); err == nil {
		t.Error("--ci none wrote a workflow")
	}
}
