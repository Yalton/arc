package initcmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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

// ArcVersion is the version of the running arc. cmd/arc sets it; the
// workflow installs that version, or latest when it is not a release.
var ArcVersion = "dev"

// releaseVersion matches release tags 'go install' can fetch: vX.Y.Z with an
// optional dash-free prerelease. Pseudo-versions and "+dirty" builds fail it.
var releaseVersion = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$`)

// installVersion is the version the workflow pins, and whether it is a
// release (false means "latest").
func installVersion() (string, bool) {
	if releaseVersion.MatchString(ArcVersion) {
		return ArcVersion, true
	}
	return "latest", false
}

// ciModule is the package the workflow installs.
const ciModule = "github.com/Yalton/arc/cmd/arc"

// ciWorkflow is the workflow text. %[1]s is the provider, %[2]s the default
// branch, %[3]s the module path and %[4]s the version to install.
const ciWorkflow = ciMarker + ` %[1]s'; rerunning it overwrites this file.
# Delete the line above to take ownership of the file.
#
# Installs the arc that wrote this file; rerun 'arc init --ci %[1]s' after
# upgrading arc to move the pin.
name: arc
on:
  push:
    branches: [%[2]q]
  pull_request:
    # labeled/unlabeled rerun the gate when a maintainer adds or removes
    # arch-approved, which approves structural registry changes.
    types: [opened, synchronize, reopened, labeled, unlabeled]

jobs:
  gate:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0 # arc gate --base needs the base branch history
      - uses: actions/setup-go@v5
        with:
          go-version: stable
          cache: false
      - name: Install arc
        run: go install %[3]s@%[4]s
      - name: arc gate (pull request)
        if: github.event_name == 'pull_request'
        env:
          BASE_REF: ${{ github.base_ref }}
          ARC_APPROVED: ${{ contains(github.event.pull_request.labels.*.name, 'arch-approved') && '1' || '0' }}
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
	pin, release := installVersion()
	want := fmt.Sprintf(ciWorkflow, provider, branch, ciModule, pin)

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
	if !release {
		res.notice("%s installs arc@latest because this arc (%s) is not a release build; rerun init with a released arc to pin it", rel, ArcVersion)
	}
	if provider == "gitea" {
		res.notice("Forgejo also reads .gitea/workflows")
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
