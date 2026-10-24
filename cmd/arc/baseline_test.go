package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBaselineWritesFile(t *testing.T) {
	tool := filepath.Join(t.TempDir(), "fake-tool")
	if err := os.WriteFile(tool, []byte("#!/bin/sh\nprintf 'b.go:2:1: two\\na.go:7:3: one\\na.go:9:3: one\\n'\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", root).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	reg := "version: 1\nsystem: {name: x, summary: x}\nchecks:\n" +
		"  - id: lint\n    command: [" + tool + "]\n    baseline: true\n" +
		"  - id: strict\n    command: [" + tool + "]\n    baseline: false\n"
	if err := os.MkdirAll(filepath.Join(root, ".arch"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".arch", "registry.yaml"), []byte(reg), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	code, out, errOut := runArc("baseline", "lint")
	if code != exitOK || !strings.Contains(out, "checks.lint: 2 finding(s) accepted in .arch/baseline/lint.txt (2 added, 0 removed)") {
		t.Fatalf("code %d out %q err %q", code, out, errOut)
	}
	data, _ := os.ReadFile(filepath.Join(root, ".arch", "baseline", "lint.txt"))
	if string(data) != "a.go: one\nb.go: two\n" {
		t.Fatalf("baseline %q", data)
	}

	code, out, _ = runArc("--json", "baseline", "checks.lint")
	var res struct{ Findings, Added, Removed int }
	if code != exitOK || json.Unmarshal([]byte(out), &res) != nil || res.Findings != 2 || res.Added != 0 {
		t.Fatalf("code %d out %q", code, out)
	}

	if code, _, errOut = runArc("gate", "--all", "--only", "checks.lint"); code != exitOK {
		t.Fatalf("gate after baseline: code %d err %q", code, errOut)
	}
	for _, args := range [][]string{{"baseline"}, {"baseline", "nope"}, {"baseline", "strict"}} {
		if code, _, _ := runArc(args...); code != exitUsage {
			t.Errorf("%v: code %d", args, code)
		}
	}
}
