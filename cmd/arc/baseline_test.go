package main

import (
	"encoding/json"
	"fmt"
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
	want := "checks.lint: 3 finding(s) accepted in .arch/baseline/lint.txt (3 added, 0 removed)\n" +
		"newly accepted:\n  a.go: one\n  a.go: one\n  b.go: two\n"
	if code != exitOK || out != want {
		t.Fatalf("code %d out %q err %q", code, out, errOut)
	}
	data, _ := os.ReadFile(filepath.Join(root, ".arch", "baseline", "lint.txt"))
	if string(data) != "a.go: one\na.go: one\nb.go: two\n" {
		t.Fatalf("baseline %q", data)
	}

	code, out, _ = runArc("--json", "baseline", "checks.lint")
	var res struct {
		Findings, Added, Removed int
		AddedSample              []string `json:"added_sample"`
	}
	if code != exitOK || json.Unmarshal([]byte(out), &res) != nil || res.Findings != 3 || res.Added != 0 ||
		res.AddedSample == nil || len(res.AddedSample) != 0 {
		t.Fatalf("code %d out %q", code, out)
	}

	if code, _, errOut = runArc("gate", "--all", "--only", "checks.lint"); code != exitOK {
		t.Fatalf("gate after baseline: code %d err %q", code, errOut)
	}
	// Many new findings: the sample is capped, and --json carries it.
	var many strings.Builder
	for i := 0; i < 7; i++ {
		fmt.Fprintf(&many, "printf 'c%d.go:1: new\\n'\n", i)
	}
	if err := os.WriteFile(tool, []byte("#!/bin/sh\n"+many.String()+"exit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	code, out, _ = runArc("--json", "baseline", "lint")
	if code != exitOK || json.Unmarshal([]byte(out), &res) != nil || res.Added != 7 || res.Removed != 3 ||
		strings.Join(res.AddedSample, ",") != "c0.go: new,c1.go: new,c2.go: new,c3.go: new,c4.go: new" {
		t.Fatalf("code %d out %q", code, out)
	}

	for _, args := range [][]string{{"baseline"}, {"baseline", "nope"}, {"baseline", "strict"}} {
		if code, _, _ := runArc(args...); code != exitUsage {
			t.Errorf("%v: code %d", args, code)
		}
	}
}
