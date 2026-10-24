package hook

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// opencodeRun feeds a fixture (with {{ROOT}} replaced) to the opencode adapter.
func opencodeRun(t *testing.T, root, fixture string) (reply opencodeReply, raw, stderr string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "hooks", "opencode", fixture))
	if err != nil {
		t.Fatal(err)
	}
	payload := strings.ReplaceAll(string(data), "{{ROOT}}", root)
	var out, errb bytes.Buffer
	if code := Opencode(strings.NewReader(payload), &out, &errb, now); code != 0 {
		t.Fatalf("exit code %d", code)
	}
	if out.Len() > 0 {
		if err := json.Unmarshal(out.Bytes(), &reply); err != nil {
			t.Fatalf("reply is not JSON: %q", out.String())
		}
	}
	return reply, out.String(), errb.String()
}

func TestOpencodeWriteUnownedDenied(t *testing.T) {
	for _, f := range []string{"pre-write-unowned.json", "pre-patch-unowned.json"} {
		r, _, _ := opencodeRun(t, newRepo(t), f)
		if r.Action != "deny" || !strings.Contains(r.Reason, "internal/foo/x.go: unowned") {
			t.Fatalf("%s: got %+v", f, r)
		}
	}
}

func TestOpencodeOwnedEditContextOnce(t *testing.T) {
	root := newRepo(t)
	r, _, _ := opencodeRun(t, root, "pre-edit-owned.json")
	if r.Action != "allow" || r.Reason != "" || !strings.Contains(r.Context, "Goals are work nodes.") {
		t.Fatalf("want context, got %+v", r)
	}
	if _, raw, _ := opencodeRun(t, root, "pre-edit-owned.json"); raw != "" {
		t.Fatalf("second edit in same session must be silent, got %q", raw)
	}
}

func TestOpencodeMalformedPayloadAllowed(t *testing.T) {
	_, raw, stderr := opencodeRun(t, newRepo(t), "malformed.json")
	if raw != "" || !strings.Contains(stderr, "malformed payload") {
		t.Fatalf("stdout %q stderr %q", raw, stderr)
	}
}

func TestOpencodeBdCreateTriaged(t *testing.T) {
	root := newRepo(t)
	state := fakeBd(t)
	fakeShowBead(t, state, "arc-1", "Concepts touched: goal")
	r, _, stderr := opencodeRun(t, root, "post-shell-bd-create.json")
	if r.Action != "allow" || !strings.Contains(r.Context, "arc-1: arch:review") {
		t.Fatalf("got %+v stderr %q", r, stderr)
	}
}

func TestOpencodeEventMapping(t *testing.T) {
	cases := []struct {
		payload string
		kind    Kind
		paths   []string
		cwd     string
		output  string
	}{
		{`{"hook":"execute.before","tool":"multiedit","directory":"/r","input":{"filePath":"a.go"}}`, PreWrite, []string{"a.go"}, "/r", ""},
		{`{"hook":"execute.before","tool":"read","directory":"/r","input":{"path":"a.go"}}`, Other, nil, "/r", ""},
		{`{"hook":"execute.before","tool":"shell","directory":"/r","input":{"command":"bd create x"}}`, Other, nil, "/r", ""},
		{`{"hook":"execute.after","tool":"write","directory":"/r","input":{"path":"a.go"}}`, Other, nil, "/r", ""},
		{`{"hook":"execute.after","tool":"bash","directory":"/r","input":{"command":"x","workdir":"sub"},"result":{"content":"out"}}`, PostShell, nil, "/r/sub", "out"},
		{`{"hook":"execute.after","tool":"shell","directory":"/r","input":{"command":"x","workdir":"/w"},"result":{"content":[{"type":"text","text":"a"},{"type":"file","uri":"u"},{"type":"text","text":"b"}]}}`, PostShell, nil, "/w", "a\nb"},
	}
	for _, c := range cases {
		ev, _, err := OpencodeEvent([]byte(c.payload))
		if err != nil || ev.Kind != c.kind || strings.Join(ev.Paths, ",") != strings.Join(c.paths, ",") ||
			ev.Cwd != c.cwd || ev.Output != c.output {
			t.Errorf("%s: event %+v err %v", c.payload, ev, err)
		}
	}
}

func TestOpencodeDenyOnlyBefore(t *testing.T) {
	// A write reported after the fact cannot be denied; it maps to nothing.
	root := newRepo(t)
	payload := `{"hook":"execute.after","tool":"write","directory":` + mustJSON(root) + `,"input":{"path":"internal/foo/x.go"}}`
	var out, errb bytes.Buffer
	Opencode(strings.NewReader(payload), &out, &errb, now)
	if out.Len() != 0 {
		t.Fatalf("got %q", out.String())
	}
}
