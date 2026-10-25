package initcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Yalton/arc/adapters/opencode"
)

func TestEnsureOpencodeInstallsIdempotently(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".opencode", "plugins", "arc.js")
	res := &Result{}
	if err := ensureOpencode(root, res); err != nil {
		t.Fatal(err)
	}
	if len(res.Created) != 1 || res.Created[0] != ".opencode/plugins/arc.js" || len(res.Notices) != 1 {
		t.Fatalf("created %v notices %v", res.Created, res.Notices)
	}
	if data, _ := os.ReadFile(path); string(data) != string(opencode.Shim) {
		t.Fatal("shim content differs")
	}

	res = &Result{}
	if err := ensureOpencode(root, res); err != nil || len(res.Created)+len(res.Updated)+len(res.Notices) != 0 {
		t.Fatalf("rerun: %+v err %v", res, err)
	}

	// An older arc shim is refreshed.
	os.WriteFile(path, []byte(opencode.Marker+" old\n"), 0o644)
	res = &Result{}
	if err := ensureOpencode(root, res); err != nil || len(res.Updated) != 1 {
		t.Fatalf("refresh: %+v err %v", res, err)
	}
	if data, _ := os.ReadFile(path); string(data) != string(opencode.Shim) {
		t.Fatal("old shim not replaced")
	}
}

func TestEnsureOpencodeKeepsForeignFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".opencode", "plugins", "arc.js")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte("export default {}\n"), 0o644)
	res := &Result{}
	if err := ensureOpencode(root, res); err != nil {
		t.Fatal(err)
	}
	if len(res.Created)+len(res.Updated) != 0 || len(res.Notices) != 1 || !strings.Contains(res.Notices[0], "not arc's") {
		t.Fatalf("%+v", res)
	}
	if data, _ := os.ReadFile(path); string(data) != "export default {}\n" {
		t.Fatal("foreign file overwritten")
	}
}

func TestShimStartsWithMarker(t *testing.T) {
	if !strings.HasPrefix(string(opencode.Shim), opencode.Marker) {
		t.Fatal("adapters/opencode/arc.js must start with opencode.Marker")
	}
}
