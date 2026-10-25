package initcmd

import (
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

var update = flag.Bool("update", false, "rewrite golden files in testdata/ci")

// pinVersion makes the workflow pin v for the rest of the test.
func pinVersion(t *testing.T, v string) {
	old := ArcVersion
	ArcVersion = v
	t.Cleanup(func() { ArcVersion = old })
}

func TestCIWorkflowGolden(t *testing.T) {
	pinVersion(t, "v1.2.3")
	for provider, rel := range ciPaths {
		t.Run(provider, func(t *testing.T) {
			root := newRepo(t, "main.go")
			res, err := Run(root, Options{CI: provider})
			if err != nil {
				t.Fatal(err)
			}
			if !contains(res.Created, rel) {
				t.Fatalf("created = %v, want %s", res.Created, rel)
			}
			got, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
			if err != nil {
				t.Fatal(err)
			}
			golden := filepath.Join("..", "..", "testdata", "ci", provider+".yml")
			if *update {
				if err := os.WriteFile(golden, got, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != string(want) {
				t.Errorf("%s mismatch (run with -update to accept):\n%s", rel, got)
			}
			var parsed map[string]any
			if err := yaml.Unmarshal(got, &parsed); err != nil || parsed["jobs"] == nil {
				t.Fatalf("workflow is not valid YAML: %v", err)
			}
			if !anyContains(res.Notices, "guess") || anyContains(res.Notices, "latest") {
				t.Errorf("notices = %v", res.Notices)
			}
		})
	}
}

func TestCIInstallVersion(t *testing.T) {
	for _, c := range []struct{ arc, want string }{
		{"v1.2.3", "@v1.2.3"},
		{"v2.0.0-rc.1", "@v2.0.0-rc.1"},
		{"dev", "@latest"},
		{"v0.0.0-20261024120000-abcdef123456", "@latest"},
		{"v1.2.3+dirty", "@latest"},
	} {
		t.Run(c.arc, func(t *testing.T) {
			pinVersion(t, c.arc)
			root := newRepo(t, "main.go")
			res, err := Run(root, Options{CI: "github"})
			if err != nil {
				t.Fatal(err)
			}
			data, _ := os.ReadFile(filepath.Join(root, ".github", "workflows", "arc.yml"))
			if !strings.Contains(string(data), "run: go install "+ciModule+c.want+"\n") {
				t.Fatalf("workflow does not install %s:\n%s", c.want, data)
			}
			if latest := c.want == "@latest"; anyContains(res.Notices, "arc@latest") != latest {
				t.Errorf("latest notice = %v, want %v: %v", !latest, latest, res.Notices)
			}
		})
	}
}

func TestCIIdempotentAndRefreshesStale(t *testing.T) {
	root := newRepo(t, "main.go")
	if _, err := Run(root, Options{CI: "github"}); err != nil {
		t.Fatal(err)
	}
	full := filepath.Join(root, ".github", "workflows", "arc.yml")
	first, _ := os.ReadFile(full)

	res, err := Run(root, Options{CI: "github"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Created)+len(res.Updated) != 0 {
		t.Fatalf("rerun changed %v %v", res.Created, res.Updated)
	}

	write(t, root, ".github/workflows/arc.yml", ciMarker+" github'; old\nname: stale\n")
	res, err = Run(root, Options{CI: "github"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(res.Updated, []string{".github/workflows/arc.yml"}) {
		t.Fatalf("updated = %v", res.Updated)
	}
	if again, _ := os.ReadFile(full); string(again) != string(first) {
		t.Fatal("stale workflow not refreshed")
	}
}

func TestCIRefusesForeignFile(t *testing.T) {
	root := newRepo(t, "main.go")
	foreign := "name: mine\non: push\n"
	write(t, root, ".gitea/workflows/arc.yml", foreign)
	_, err := Run(root, Options{CI: "gitea"})
	if err == nil || !strings.Contains(err.Error(), "not managed by arc") {
		t.Fatalf("err = %v", err)
	}
	if data, _ := os.ReadFile(filepath.Join(root, ".gitea", "workflows", "arc.yml")); string(data) != foreign {
		t.Fatal("foreign workflow was modified")
	}
}

func TestCIUsesOriginHEAD(t *testing.T) {
	root := newRepo(t, "main.go")
	git(t, root, "update-ref", "refs/remotes/origin/trunk", "HEAD")
	git(t, root, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/trunk")
	res, err := Run(root, Options{CI: "github"})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(root, ".github", "workflows", "arc.yml"))
	if !strings.Contains(string(data), `branches: ["trunk"]`) {
		t.Fatalf("workflow does not target trunk:\n%s", data)
	}
	if anyContains(res.Notices, "guess") {
		t.Fatalf("unexpected guess notice: %v", res.Notices)
	}
}

func TestCIRejectsUnknownProviderBeforeWriting(t *testing.T) {
	root := newRepo(t, "main.go")
	if _, err := Run(root, Options{CI: "gitlab"}); err == nil || !strings.Contains(err.Error(), "github or gitea") {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".arch")); !os.IsNotExist(err) {
		t.Fatal("init wrote files despite a bad --ci value")
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func anyContains(list []string, sub string) bool {
	for _, x := range list {
		if strings.Contains(x, sub) {
			return true
		}
	}
	return false
}
