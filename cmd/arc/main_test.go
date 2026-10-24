package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func runArc(args ...string) (code int, stdout, stderr string) {
	var o, e bytes.Buffer
	code = run(args, &o, &e)
	return code, o.String(), e.String()
}

func TestVersion(t *testing.T) {
	code, out, _ := runArc("version")
	if code != exitOK || !strings.HasPrefix(out, "arc ") {
		t.Fatalf("version: code %d, out %q", code, out)
	}
}

func TestVersionJSON(t *testing.T) {
	for _, args := range [][]string{{"--json", "version"}, {"version", "--json"}} {
		code, out, _ := runArc(args...)
		var v struct{ Version string }
		if code != exitOK || json.Unmarshal([]byte(out), &v) != nil || v.Version == "" {
			t.Fatalf("%v: code %d, out %q", args, code, out)
		}
	}
}

func TestUnknownCommandExits2(t *testing.T) {
	code, _, errOut := runArc("bogus")
	if code != exitUsage || !strings.Contains(errOut, `unknown command "bogus"`) {
		t.Fatalf("code %d, stderr %q", code, errOut)
	}
}

func TestNoArgsExits2(t *testing.T) {
	if code, _, _ := runArc(); code != exitUsage {
		t.Fatalf("code %d", code)
	}
}

func TestUnimplementedExits2(t *testing.T) {
	for _, c := range commands {
		if _, ok := handlers[c]; ok {
			continue
		}
		code, _, errOut := runArc(c)
		if code != exitUsage || !strings.Contains(errOut, "not implemented") {
			t.Errorf("%s: code %d, stderr %q", c, code, errOut)
		}
	}
}

func TestUnimplementedJSONError(t *testing.T) {
	code, out, _ := runArc("audit", "--json")
	var v struct{ Error string }
	if code != exitUsage || json.Unmarshal([]byte(out), &v) != nil || v.Error == "" {
		t.Fatalf("code %d, out %q", code, out)
	}
}

func TestExtractJSONFlagStopsAtDoubleDash(t *testing.T) {
	args, found := extractJSONFlag([]string{"place", "--", "--json"})
	if found || len(args) != 3 {
		t.Fatalf("args %v, found %v", args, found)
	}
}

func TestHookUnsupportedHarnessNeverBlocks(t *testing.T) {
	for _, args := range [][]string{{"hook"}, {"hook", "opencode"}, {"hook", "bogus"}} {
		if code, _, errOut := runArc(args...); code != exitOK || !strings.Contains(errOut, "unsupported harness") {
			t.Errorf("%v: code %d stderr %q", args, code, errOut)
		}
	}
}
