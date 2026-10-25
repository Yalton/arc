package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGateUsageErrors(t *testing.T) {
	cases := []struct {
		args      []string
		want, not string
	}{
		{[]string{"gate", "some/path"}, "unexpected arguments: some/path; gate judges the change set", "at most one"},
		{[]string{"gate", "--staged", "a", "b"}, "unexpected arguments: a b", "at most one"},
		{[]string{"gate", "--staged", "--all"}, "use at most one of --base <ref>, --staged, --all", "unexpected"},
		{[]string{"gate", "--base", "main", "--staged"}, "use at most one of", "unexpected"},
	}
	for _, c := range cases {
		code, _, errOut := runArc(c.args...)
		if code != exitUsage || !strings.Contains(errOut, c.want) || strings.Contains(errOut, c.not) {
			t.Errorf("%v: exit %d, stderr %q", c.args, code, errOut)
		}
	}
}

// TestGateApprovalEnv: with --base a new concept needs approval, which
// ARC_APPROVED=1 (set by CI from the arch-approved label) grants.
func TestGateApprovalEnv(t *testing.T) {
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("ARC_APPROVED", "")
	root := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		full := append([]string{"-C", root, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)
		if out, err := exec.Command("git", full...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(root, filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	reg := "version: 1\nconcepts:\n  - id: a\n    paths: [\"a/**\"]\n"
	git("init", "-q")
	write(".arch/registry.yaml", reg)
	write("a/a.go", "package a\n")
	git("add", "-A")
	git("commit", "-q", "-m", "base")
	git("branch", "base")
	write(".arch/registry.yaml", reg+"  - id: b\n    paths: [\"b/**\"]\n    reason: B.\n")
	write("b/b.go", "package b\n")
	git("add", "-A")
	git("commit", "-q", "-m", "add b")
	t.Chdir(root)

	code, out, _ := runArc("gate", "--base", "base", "--only", "approval")
	if code != exitViolations || !strings.Contains(out, "needs human approval: concept b added.") {
		t.Fatalf("unapproved: code %d\n%s", code, out)
	}
	if code, out, _ := runArc("gate", "--staged", "--only", "approval"); code != exitOK || out != "gate: ok\n" {
		t.Fatalf("--staged must not run approval: code %d\n%s", code, out)
	}
	t.Setenv("ARC_APPROVED", "1")
	code, out, _ = runArc("gate", "--base", "base", "--only", "approval")
	if code != exitOK || !strings.Contains(out, "[approval] info: .arch/registry.yaml: skipped: ARC_APPROVED=1") {
		t.Fatalf("approved: code %d\n%s", code, out)
	}
}
