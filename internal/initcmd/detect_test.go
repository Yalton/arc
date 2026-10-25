package initcmd

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Yalton/arc/internal/registry"
)

// fakeBin returns a PATH list of one directory holding executables names.
func fakeBin(t *testing.T, names ...string) string {
	t.Helper()
	bin := t.TempDir()
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(bin, n), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return bin
}

func findings(fs []Finding) []string {
	var out []string
	for _, f := range fs {
		out = append(out, f.Item+"="+f.Value+" "+f.Source)
	}
	return out
}

func TestResolveDetectsEverything(t *testing.T) {
	root := newRepo(t, "main.go", "internal/a/a.go")
	write(t, root, ".codex/config.toml", "")
	git(t, root, "remote", "add", "origin", "https://github.com/x/y.git")
	bin := fakeBin(t, "claude", "dupl")
	write(t, bin, "opencode", "not executable")

	opts, fs := Resolve(root, bin, Options{}, nil)
	want := Options{Scan: true, Claude: true, Codex: true, CI: "github", CIDetected: true, Dupl: true}
	if opts != want {
		t.Errorf("opts = %+v, want %+v", opts, want)
	}
	got := findings(fs)
	wantF := []string{"claude=on detected", "codex=on detected", "opencode=off detected", "ci=github detected", "scan=on detected", "beads=off detected", "dupl=on detected", "jscpd=off detected"}
	if !reflect.DeepEqual(got, wantF) {
		t.Errorf("findings = %v, want %v", got, wantF)
	}
	reasons := map[string]string{}
	for _, f := range fs {
		reasons[f.Item] = f.Reason
	}
	if reasons["claude"] != "claude on PATH ("+filepath.Join(bin, "claude")+")" ||
		reasons["codex"] != ".codex/ exists" ||
		reasons["ci"] != "origin host is github.com" ||
		reasons["scan"] != "new registry and 2 file(s) to scan" {
		t.Errorf("reasons = %v", reasons)
	}

	res, err := Run(root, opts)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{".claude/settings.json", ".codex/hooks.json", ".github/workflows/arc.yml"} {
		if !contains(res.Created, p) {
			t.Errorf("%s not created: %v", p, res.Created)
		}
	}
	if len(validate(t, root).Concepts) == 0 {
		t.Error("scan proposed no concepts")
	}
	if _, err := os.Stat(filepath.Join(root, ".opencode")); err == nil {
		t.Error("opencode installed without detection")
	}

	// Rerun: the registry exists now, so scan is off, and nothing changes.
	opts, fs = Resolve(root, bin, Options{}, nil)
	if opts.Scan || fs[4].Reason != registry.Path+" exists" {
		t.Errorf("scan on rerun: %+v %+v", opts, fs[4])
	}
	res, err = Run(root, opts)
	if err != nil || len(res.Created)+len(res.Updated) != 0 {
		t.Errorf("rerun changed: %v %v %v", err, res.Created, res.Updated)
	}
}

func TestResolveNothingDetected(t *testing.T) {
	root := newRepo(t, "README.md")
	opts, fs := Resolve(root, fakeBin(t), Options{}, nil)
	if opts != (Options{}) {
		t.Errorf("opts = %+v", opts)
	}
	want := []string{
		"no .claude/ and claude not on PATH",
		"no .codex/ and codex not on PATH",
		"no .opencode/ and opencode not on PATH",
		"no origin remote",
		"new registry but no files to scan",
		"no .beads/ directory",
		"no Go files",
		"jscpd not on PATH; install it with 'npm install -g jscpd' and rerun arc init",
	}
	if len(fs) != len(want) {
		t.Fatalf("findings = %v", findings(fs))
	}
	for i, f := range fs {
		if f.Reason != want[i] {
			t.Errorf("%s reason = %q, want %q", f.Item, f.Reason, want[i])
		}
	}
}

func TestBeadsDetectedForNewRegistry(t *testing.T) {
	root := newRepo(t, "main.go")
	write(t, root, ".beads/config.yaml", "")
	opts, fs := Resolve(root, fakeBin(t), Options{}, nil)
	if !opts.Beads || fs[5].Item != "beads" || fs[5].Reason != ".beads/ exists" {
		t.Fatalf("opts %+v, finding %+v", opts, fs[5])
	}
	if _, err := Run(root, opts); err != nil {
		t.Fatal(err)
	}
	if reg := validate(t, root); !reg.Beads {
		t.Error("new registry lacks beads: true")
	}
	if !strings.Contains(readFile(t, filepath.Join(root, "AGENTS.md")), "Concepts touched") {
		t.Error("snippet lacks the bead line")
	}

	// Rerun: the registry exists, so nothing is detected or rewritten.
	opts, fs = Resolve(root, fakeBin(t), Options{}, nil)
	if opts.Beads || fs[5].Reason != registry.Path+" exists" {
		t.Errorf("rerun: %+v %+v", opts, fs[5])
	}
}

func TestBeadsNoticeForExistingRegistry(t *testing.T) {
	root := newRepo(t, "main.go")
	if _, err := Run(root, Options{}); err != nil {
		t.Fatal(err)
	}
	before := readFile(t, filepath.Join(root, registry.Path))
	if strings.Contains(before, "beads: true") {
		t.Fatalf("skeleton without beads sets it:\n%s", before)
	}
	res, err := Run(root, Options{})
	if err != nil || anyContains(res.Notices, "beads: true") {
		t.Fatalf("notice without .beads/: %v %v", err, res.Notices)
	}
	write(t, root, ".beads/config.yaml", "")
	res, err = Run(root, Options{})
	if err != nil || !anyContains(res.Notices, "add 'beads: true'") {
		t.Fatalf("no beads notice: %v %v", err, res.Notices)
	}
	if got := readFile(t, filepath.Join(root, registry.Path)); got != before {
		t.Error("existing registry rewritten")
	}
}

func TestResolveExplicitFlagsWin(t *testing.T) {
	root := newRepo(t, "main.go", "internal/a/a.go")
	write(t, root, ".claude/settings.json", "{}")
	git(t, root, "remote", "add", "origin", "git@github.com:x/y.git")
	explicit := map[string]bool{"claude": true, "ci": true, "scan": true}
	opts, fs := Resolve(root, fakeBin(t, "codex"), Options{CI: "none"}, explicit)
	want := Options{Codex: true, CI: "none"}
	if opts != want {
		t.Errorf("opts = %+v, want %+v", opts, want)
	}
	got := map[string]string{}
	for _, f := range fs {
		got[f.Item] = f.Value + " " + f.Source + " " + f.Reason
	}
	if got["claude"] != "off flag --claude=false" || got["ci"] != "none flag --ci none" || got["scan"] != "off flag --scan=false" {
		t.Errorf("findings = %v", got)
	}
	res, err := Run(root, opts)
	if err != nil {
		t.Fatal(err)
	}
	if anyContains(res.Created, "workflows") || contains(res.Updated, ".claude/settings.json") {
		t.Errorf("explicit opt-outs ignored: %v %v", res.Created, res.Updated)
	}
}

func TestDetectCIHosts(t *testing.T) {
	cases := map[string]string{
		"https://github.com/x/y.git":             "github",
		"https://user:tok@GitHub.com/x/y":        "github",
		"git@github.com:x/y.git":                 "github",
		"ssh://git@git.example.com:2222/x/y.git": "gitea",
		"forgejo@codeberg.org:x/y.git":           "gitea",
		"https://gitlab.com/x/y.git":             "",
		"git@bitbucket.org:x/y.git":              "",
		"/srv/git/y.git":                         "",
		"file:///srv/git/y.git":                  "",
		"../y.git":                               "",
	}
	for url, want := range cases {
		root := newRepo(t)
		git(t, root, "remote", "add", "origin", url)
		got, why := detectCI(root)
		if got != want {
			t.Errorf("%s: provider %q (%s), want %q", url, got, why, want)
		}
		if strings.Contains(why, "tok") {
			t.Errorf("%s: reason leaks credentials: %s", url, why)
		}
	}
}

func TestDetectedCIKeepsForeignWorkflowAsNotice(t *testing.T) {
	root := newRepo(t, "main.go")
	write(t, root, ".gitea/workflows/arc.yml", "name: mine\n")
	res, err := Run(root, Options{CI: "gitea", CIDetected: true})
	if err != nil {
		t.Fatalf("detected CI over foreign workflow failed: %v", err)
	}
	if !anyContains(res.Notices, "not managed by arc; CI left unchanged") {
		t.Errorf("notices = %v", res.Notices)
	}
	if got := readFile(t, filepath.Join(root, ".gitea/workflows/arc.yml")); got != "name: mine\n" {
		t.Errorf("foreign workflow touched: %q", got)
	}
	if _, err := Run(root, Options{CI: "gitea"}); err == nil || !strings.Contains(err.Error(), "not managed by arc") {
		t.Errorf("explicit --ci over foreign workflow: %v", err)
	}
}
