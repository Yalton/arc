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

// Rule: the block uses CRLF if the file contains any CRLF, else LF.
func TestSetModuleMixedEndingsUseCRLF(t *testing.T) {
	src := "version: 1\nsystem: {name: demo}\r\nconcepts:\n  - {id: g, paths: [\"**\"]}\n"
	out, err := SetModule([]byte(src), Module{ID: "K", Summary: "K.", Entry: []string{"K"}})
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	if !strings.HasPrefix(got, src) || !strings.Contains(got, "modules:\r\n  - id: K\r\n") ||
		strings.Contains(got[len(src):], "\n") && strings.Contains(strings.ReplaceAll(got[len(src):], "\r\n", ""), "\n") {
		t.Fatalf("got %q", got)
	}
}

func TestSetModulePureLFStaysLF(t *testing.T) {
	src := "version: 1\nsystem: {name: demo}\nconcepts:\n  - {id: g, paths: [\"**\"]}\n"
	out, _ := SetModule([]byte(src), Module{ID: "K", Summary: "K.", Entry: []string{"K"}})
	if strings.Contains(string(out), "\r") {
		t.Fatalf("CR added: %q", out)
	}
}

func TestSetModuleAppendKeepsFootCommentOnce(t *testing.T) {
	data := []byte("version: 1\nsystem: {name: demo}\nconcepts:\n  - {id: g, paths: [\"**\"]}\nmodules:\n  - id: A\n    summary: A.\n    entry: [A]\n    # foot of A\n")
	for _, id := range []string{"B", "C"} {
		var err error
		if data, err = SetModule(data, Module{ID: id, Summary: id + ".", Entry: []string{id}}); err != nil {
			t.Fatal(err)
		}
	}
	if n := strings.Count(string(data), "# foot of A"); n != 1 {
		t.Fatalf("foot comment appears %d times:\n%s", n, data)
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
