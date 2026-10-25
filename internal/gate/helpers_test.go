package gate

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Yalton/arc/internal/gitx"
	"github.com/Yalton/arc/internal/registry"
)

// testRepo is a temp git repository with a registry.
type testRepo struct {
	t    *testing.T
	root string
}

func newRepo(t *testing.T, registryYAML string) *testRepo {
	t.Helper()
	r := &testRepo{t: t, root: t.TempDir()}
	r.git("init", "-q")
	r.write(registry.Path, registryYAML)
	return r
}

func (r *testRepo) git(args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", append([]string{"-C", r.root, "-c", "user.name=t", "-c", "user.email=t@t",
		"-c", "init.defaultBranch=main", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func (r *testRepo) write(path, content string) {
	r.t.Helper()
	p := filepath.Join(r.root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

func (r *testRepo) commitAll(msg string) {
	r.git("add", "-A")
	r.git("commit", "-q", "-m", msg)
}

// render writes the arc blocks so the registry check starts clean.
func (r *testRepo) render() {
	r.t.Helper()
	reg, err := registry.Load(r.root)
	if err != nil {
		r.t.Fatal(err)
	}
	if _, err := reg.Render(r.root); err != nil {
		r.t.Fatal(err)
	}
}

// gate runs the given checks in mode "staged", "all" or a base ref.
func (r *testRepo) gate(mode string, only ...string) *Report {
	r.t.Helper()
	reg, err := registry.Load(r.root)
	if err != nil {
		r.t.Fatal(err)
	}
	files, err := registry.RepoFiles(r.root)
	if err != nil {
		r.t.Fatal(err)
	}
	ctx := &Context{Root: r.root, Reg: reg, Files: files, Now: time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)}
	baseRev := "HEAD"
	switch mode {
	case "staged":
		ctx.Staged = true
		ctx.Changes, err = gitx.StagedChangeSet(r.root)
	case "working":
		ctx.Changes, err = gitx.WorkingChangeSet(r.root)
	case "all":
		ctx.All = true
		ctx.Changes, err = WholeTree(r.root, files)
	default:
		ctx.Changes, err = gitx.BaseChangeSet(r.root, mode)
		if err == nil {
			baseRev, err = gitx.MergeBase(r.root, mode)
		}
	}
	if err != nil {
		r.t.Fatal(err)
	}
	if !ctx.All {
		if data, ok, _ := gitx.FileAt(r.root, baseRev, registry.Path); ok {
			if ctx.BaseReg, err = registry.Parse(data); err != nil {
				r.t.Fatal(err)
			}
		}
	}
	rep, err := Run(ctx, only)
	if err != nil {
		r.t.Fatal(err)
	}
	return rep
}

func findings(rep *Report) string {
	var s []string
	for _, f := range rep.Findings {
		s = append(s, f.String())
	}
	return strings.Join(s, "\n")
}
