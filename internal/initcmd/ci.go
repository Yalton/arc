package initcmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ciMarker is the first line of every workflow 'arc init --ci' writes. A
// file with it is arc's to refresh; a file without it is never touched.
const ciMarker = "# Managed by 'arc init --ci"

// ciPaths maps each --ci provider to its workflow file. Forgejo reads
// .forgejo/workflows and .gitea/workflows, so "gitea" serves both.
var ciPaths = map[string]string{
	"github": ".github/workflows/arc.yml",
	"gitea":  ".gitea/workflows/arc.yml",
}

// checkCI rejects an unknown --ci provider before anything is written.
func checkCI(provider string) error {
	if _, ok := ciPaths[provider]; provider != "" && !ok {
		return fmt.Errorf("--ci must be github or gitea, not %q", provider)
	}
	return nil
}

// ciWorkflow is the workflow text. arc's module path is "arc", so it cannot
// be fetched with 'go install'; the workflow builds it from the git URL in
// the ARC_REPO repository variable at ARC_REF instead. %[1]s is the
// provider, %[2]s the default branch.
const ciWorkflow = ciMarker + ` %[1]s'; rerunning it overwrites this file.
# Delete the line above to take ownership of the file.
#
# arc cannot be 'go install'ed, so this workflow builds it from source. Set
# repository variables (Settings > Actions > Variables):
#   ARC_REPO  git URL of arc's source repository (required)
#   ARC_REF   branch or tag to build (optional, default main)
name: arc
on:
  push:
    branches: [%[2]q]
  pull_request:

jobs:
  gate:
    runs-on: ubuntu-latest
    env:
      ARC_REPO: ${{ vars.ARC_REPO }}
      ARC_REF: ${{ vars.ARC_REF || 'main' }}
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0 # arc gate --base needs the base branch history
      - uses: actions/setup-go@v5
        with:
          go-version: stable
          cache: false
      - name: Install arc
        run: |
          if [ -z "$ARC_REPO" ]; then
            echo "::error::set the ARC_REPO repository variable to arc's git URL"
            exit 1
          fi
          git clone -q --depth 1 --branch "$ARC_REF" "$ARC_REPO" "$RUNNER_TEMP/arc-src"
          (cd "$RUNNER_TEMP/arc-src" && go build -o "$RUNNER_TEMP/arc-bin/arc" ./cmd/arc)
          echo "$RUNNER_TEMP/arc-bin" >> "$GITHUB_PATH"
      - name: arc gate (pull request)
        if: github.event_name == 'pull_request'
        env:
          BASE_REF: ${{ github.base_ref }}
        run: arc gate --base "origin/$BASE_REF"
      - name: arc gate (whole tree)
        if: github.event_name != 'pull_request'
        run: arc gate --all
`

// ensureCI writes or refreshes the workflow for provider. An existing file
// without ciMarker is someone else's and makes init fail without touching it.
func ensureCI(root, provider string, res *Result) error {
	rel := ciPaths[provider]
	full := filepath.Join(root, filepath.FromSlash(rel))
	branch, known := defaultBranch(root)
	want := fmt.Sprintf(ciWorkflow, provider, branch)

	data, err := os.ReadFile(full)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(full, []byte(want), 0o644); err != nil {
			return err
		}
		res.created(rel)
	case err != nil:
		return err
	case !strings.HasPrefix(string(data), ciMarker):
		return fmt.Errorf("%s exists and is not managed by arc; move it aside or add the arc gate steps by hand", rel)
	case string(data) != want:
		if err := os.WriteFile(full, []byte(want), 0o644); err != nil {
			return err
		}
		res.updated(rel)
	}
	if !known {
		res.notice("%s runs arc gate --all on pushes to %q; no origin/HEAD, so that branch is a guess: edit it if wrong", rel, branch)
	}
	res.notice("%s builds arc from source: set repository variables ARC_REPO (arc's git URL) and optionally ARC_REF (branch or tag, default main)", rel)
	if provider == "gitea" {
		res.notice("Forgejo also reads .gitea/workflows; repository variables need Gitea 1.20+ or Forgejo")
	}
	return nil
}

// defaultBranch returns the branch origin/HEAD points at, or "main" and false
// when it is unknown.
func defaultBranch(root string) (string, bool) {
	out, err := exec.Command("git", "-C", root, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD").Output()
	if err != nil {
		return "main", false
	}
	b := strings.TrimPrefix(strings.TrimSpace(string(out)), "origin/")
	if b == "" {
		return "main", false
	}
	return b, true
}
