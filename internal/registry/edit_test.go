package registry

import (
	"strings"
	"testing"
)

func TestSetModuleKeepsCRLF(t *testing.T) {
	src := "version: 1\r\nsystem: {name: demo}\r\nconcepts:\r\n  - {id: g, paths: [\"**\"]}\r\n"
	out, err := SetModule([]byte(src), Module{ID: "K", Summary: "K.", Entry: []string{"K"}})
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	if strings.Contains(strings.ReplaceAll(got, "\r\n", ""), "\n") {
		t.Fatalf("bare LF in CRLF file:\n%q", got)
	}
	if !strings.HasPrefix(got, src) || !strings.Contains(got, "modules:\r\n") {
		t.Fatalf("got %q", got)
	}
}

func TestSetModuleKeepsLFWhenMixedMostlyLF(t *testing.T) {
	src := "version: 1\nsystem: {name: demo}\nconcepts:\n  - {id: g, paths: [\"**\"]}\n"
	out, _ := SetModule([]byte(src), Module{ID: "K", Summary: "K.", Entry: []string{"K"}})
	if strings.Contains(string(out), "\r") {
		t.Fatalf("CR added: %q", out)
	}
}

func TestSetModuleKeepsIndentedFootComment(t *testing.T) {
	src := "version: 1\nsystem: {name: demo}\nconcepts:\n  - {id: g, paths: [\"**\"]}\nmodules:\n  - id: A\n    summary: A.\n    entry: [A]\n    # foot of A\n"
	out, err := SetModule([]byte(src), Module{ID: "A", Summary: "A2.", Entry: []string{"A"}})
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	if !strings.HasSuffix(got, "    # foot of A\n") || strings.Count(got, "# foot of A") != 1 {
		t.Fatalf("foot comment moved or lost:\n%s", got)
	}
}
