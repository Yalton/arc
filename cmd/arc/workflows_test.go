package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestRepoWorkflowsParse checks arc's own CI and release workflows are valid
// YAML with jobs, and that no run: script interpolates ${{ }} directly
// (values reach scripts through env instead).
func TestRepoWorkflowsParse(t *testing.T) {
	var files []string
	for _, dir := range []string{".github/workflows", ".forgejo/workflows"} {
		m, _ := filepath.Glob(filepath.Join("..", "..", dir, "*.yml"))
		files = append(files, m...)
	}
	if len(files) < 3 {
		t.Fatalf("found workflows %v, want ci and release", files)
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var wf struct {
			Jobs map[string]struct {
				Steps []struct {
					Run string `yaml:"run"`
				} `yaml:"steps"`
			} `yaml:"jobs"`
		}
		if err := yaml.Unmarshal(data, &wf); err != nil || len(wf.Jobs) == 0 {
			t.Fatalf("%s: not a valid workflow: %v", f, err)
		}
		for name, job := range wf.Jobs {
			for i, s := range job.Steps {
				if strings.Contains(s.Run, "${{") {
					t.Errorf("%s: job %s step %d interpolates ${{ }} into run:; pass it through env", f, name, i)
				}
			}
		}
	}
}
