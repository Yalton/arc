package initcmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Yalton/arc/internal/registry"
)

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "init.defaultBranch=main"}, args...)
	cmd := exec.Command("git", full...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func write(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// newRepo makes a git repository with the given files committed.
func newRepo(t *testing.T, files ...string) string {
	t.Helper()
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	root := filepath.Join(t.TempDir(), "myrepo")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, root, "init", "-q")
	for _, f := range files {
		write(t, root, f, "x\n")
	}
	if len(files) > 0 {
		git(t, root, "add", "-A")
		git(t, root, "commit", "-q", "-m", "init")
	}
	return root
}

// snapshot reads every regular file under root except .git internals other
// than hooks, keyed by relative path, with mode bits.
func snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		if d.IsDir() && rel == ".git" {
			return filepath.SkipDir
		}
		if d.Type().IsRegular() {
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			fi, _ := d.Info()
			out[rel] = fi.Mode().String() + "\n" + string(data)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	hooks, _ := filepath.Glob(filepath.Join(root, ".git", "hooks", "pre-commit*"))
	for _, h := range hooks {
		data, _ := os.ReadFile(h)
		fi, _ := os.Stat(h)
		rel, _ := filepath.Rel(root, h)
		out[rel] = fi.Mode().String() + "\n" + string(data)
	}
	return out
}

func validate(t *testing.T, root string) *registry.Registry {
	t.Helper()
	reg, err := registry.Load(root)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	files, err := registry.RepoFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	if ps := reg.Validate(files); len(ps) > 0 {
		t.Fatalf("validate: %v", ps)
	}
	return reg
}

func TestFreshInit(t *testing.T) {
	root := newRepo(t, "main.go", "internal/a/a.go")
	res, err := Run(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{registry.Path, gitignorePath, "ARCHITECTURE.md", "AGENTS.md", ".git/hooks/pre-commit"}
	if !reflect.DeepEqual(res.Created, want) {
		t.Errorf("created = %v, want %v", res.Created, want)
	}
	if len(res.Updated) != 0 {
		t.Errorf("updated = %v", res.Updated)
	}
	reg := validate(t, root)
	if reg.System.Name != "myrepo" || reg.Limits.FileLines != 1000 || reg.Limits.FunctionLines != 150 || len(reg.Concepts) != 0 {
		t.Errorf("unexpected registry: %+v", reg)
	}
	if !reg.IsIgnored("internal/a/a_test.go") || !reg.IsIgnored("web/x.spec.ts") || !reg.IsIgnored("pkg/test_x.py") {
		t.Error("default ignores missing test patterns")
	}
	gi, _ := os.ReadFile(filepath.Join(root, ".arch", ".gitignore"))
	if string(gi) != "cache/\n" {
		t.Errorf(".arch/.gitignore = %q", gi)
	}
	for _, f := range []string{"ARCHITECTURE.md", "AGENTS.md"} {
		data, _ := os.ReadFile(filepath.Join(root, f))
		if !strings.Contains(string(data), registry.BeginMarker) {
			t.Errorf("%s has no arc block", f)
		}
	}
	fi, err := os.Stat(filepath.Join(root, ".git", "hooks", "pre-commit"))
	if err != nil || fi.Mode()&0o111 == 0 {
		t.Fatalf("hook not installed executable: %v %v", fi, err)
	}
	if stale, _ := reg.IsStale(root); stale {
		t.Error("render stale after init")
	}
}

func TestRerunIdempotent(t *testing.T) {
	root := newRepo(t, "internal/a/a.go", "cmd/arc/main.go")
	write(t, root, "CLAUDE.md", "# Claude\n")
	if _, err := Run(root, Options{Scan: true}); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, root)
	res, err := Run(root, Options{Scan: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Created)+len(res.Updated) != 0 {
		t.Errorf("rerun changed: created %v updated %v", res.Created, res.Updated)
	}
	if !reflect.DeepEqual(before, snapshot(t, root)) {
		t.Error("rerun changed file bytes")
	}
	joined := strings.Join(res.Notices, "\n")
	if !strings.Contains(joined, "left unchanged") || !strings.Contains(joined, "--scan ignored") {
		t.Errorf("notices = %v", res.Notices)
	}
}

func TestNeverOverwritesRegistry(t *testing.T) {
	root := newRepo(t, "internal/a/a.go")
	custom := "version: 1\nsystem:\n  name: custom\n  summary: mine\n"
	write(t, root, registry.Path, custom)
	res, err := Run(root, Options{Scan: true})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(root, registry.Path))
	if string(got) != custom {
		t.Errorf("registry overwritten:\n%s", got)
	}
	for _, c := range res.Created {
		if c == registry.Path {
			t.Error("registry reported created")
		}
	}
	arch, _ := os.ReadFile(filepath.Join(root, "ARCHITECTURE.md"))
	if !strings.Contains(string(arch), "custom") {
		t.Error("render did not use existing registry")
	}
}

func TestScan(t *testing.T) {
	root := newRepo(t,
		"main.go", "Makefile", "README.md",
		"internal/x.go",
		"internal/a/a.go", "internal/a/a_test.go",
		"internal/b/sub/b.go",
		"internal/arc/arc.go",
		"internal/onlytests/x_test.go",
		"internal/.hidden/h.go",
		"cmd/arc/main.go",
		"web/app.js", "web/app.test.js",
		"docs/guide.txt",
		".hidden/x.go",
		"testdata/fixture.go",
		"pkg/util/u.go",
	)
	res, err := Run(root, Options{Scan: true})
	if err != nil {
		t.Fatal(err)
	}
	reg := validate(t, root)
	var got []string
	for _, c := range reg.Concepts {
		if !c.Draft || c.Summary != "TODO" || len(c.Paths) != 1 {
			t.Errorf("bad draft %+v", c)
		}
		got = append(got, c.ID+" "+c.Name+" "+c.Paths[0])
	}
	want := []string{
		"cmd-arc Cmd Arc cmd/arc/**",
		"internal Internal internal/*",
		"a A internal/a/**",
		"internal-arc Internal Arc internal/arc/**",
		"b B internal/b/**",
		"util Util pkg/util/**",
		"web Web web/**",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("concepts:\n got %q\nwant %q", got, want)
	}
	if !strings.Contains(strings.Join(res.Notices, "\n"), "left 2 root-level file(s) unowned") {
		t.Errorf("notices = %v", res.Notices)
	}
}

func TestScanOutsideGit(t *testing.T) {
	root := t.TempDir()
	write(t, root, "src/core/c.py", "x\n")
	res, err := Run(root, Options{Scan: true})
	if err != nil {
		t.Fatal(err)
	}
	reg := validate(t, root)
	if len(reg.Concepts) != 1 || reg.Concepts[0].ID != "core" {
		t.Errorf("concepts = %+v", reg.Concepts)
	}
	if !strings.Contains(strings.Join(res.Notices, "\n"), "not a git repository") {
		t.Errorf("notices = %v", res.Notices)
	}
}

func TestSkeletonParsesWithDrafts(t *testing.T) {
	text := skeleton(`we"ird`, []draft{{id: "a", name: "A", path: "a/**"}})
	reg, err := registry.Parse([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	if reg.System.Name != `we"ird` || len(reg.Concepts) != 1 || !reg.Concepts[0].Draft {
		t.Errorf("%+v", reg)
	}
}
