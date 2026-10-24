package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitCI(t *testing.T) {
	root := t.TempDir()
	cmd := exec.Command("git", "init", "-q", root)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	t.Chdir(root)

	code, out, errOut := runArc("init", "--ci", "github")
	if code != exitOK || !strings.Contains(out, "created .github/workflows/arc.yml") || !strings.Contains(out, "ARC_REPO") {
		t.Fatalf("init --ci github: code %d\nout %s\nerr %s", code, out, errOut)
	}
	if _, err := os.Stat(filepath.Join(root, ".github", "workflows", "arc.yml")); err != nil {
		t.Fatal(err)
	}

	if code, _, errOut := runArc("init", "--ci", "gitlab"); code != exitUsage || !strings.Contains(errOut, "github or gitea") {
		t.Fatalf("init --ci gitlab: code %d err %s", code, errOut)
	}

	os.MkdirAll(filepath.Join(root, ".gitea", "workflows"), 0o755)
	os.WriteFile(filepath.Join(root, ".gitea", "workflows", "arc.yml"), []byte("name: mine\n"), 0o644)
	if code, _, errOut := runArc("init", "--ci", "gitea"); code != exitUsage || !strings.Contains(errOut, "not managed by arc") {
		t.Fatalf("init --ci gitea over foreign file: code %d err %s", code, errOut)
	}
}
