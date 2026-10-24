package imports

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// fixture copies testdata/imports/<name> into a temp dir and returns it.
func fixture(t *testing.T, name string) string {
	t.Helper()
	src := filepath.Join("..", "..", "testdata", "imports", name)
	dst := t.TempDir()
	err := filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

func format(imps []Import) string {
	var s []string
	for _, i := range imps {
		s = append(s, fmt.Sprintf("%d %s -> %s", i.Line, i.Raw, i.Path))
	}
	return strings.Join(s, "\n")
}

func check(t *testing.T, got map[string][]Import, file string, want ...string) {
	t.Helper()
	if g := format(got[file]); g != strings.Join(want, "\n") {
		t.Errorf("%s:\ngot:\n%s\nwant:\n%s", file, g, strings.Join(want, "\n"))
	}
}

func TestGoImports(t *testing.T) {
	root := fixture(t, "go")
	// A file with a syntax error still reports the imports it parsed.
	if err := os.MkdirAll(filepath.Join(root, "broken"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "broken", "broken.go"),
		[]byte("package broken\n\nimport \"example.com/app/b\"\n\nfunc oops( {\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Imports(root, []string{"a/a.go", "a/single.go", "a/a_test.go", "broken/broken.go", "go.mod"})
	if err != nil {
		t.Fatal(err)
	}
	check(t, got, "a/a.go",
		"4 fmt -> ",
		"6 example.com/app/b -> b",
		"7 example.com/app/c -> c",
		"9 github.com/example/external -> ")
	check(t, got, "a/single.go", "3 example.com/app/missing -> missing")
	check(t, got, "a/a_test.go", "4 testing -> ", "6 example.com/app/c -> c")
	check(t, got, "broken/broken.go", "3 example.com/app/b -> b")
	if _, ok := got["go.mod"]; ok {
		t.Error("go.mod should not be parsed")
	}
}

func TestGoImportsNestedModule(t *testing.T) {
	root := t.TempDir()
	write := func(p, s string) {
		p = filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("svc/go.mod", "module example.com/svc\n\ngo 1.22\n")
	write("svc/cmd/main.go", "package main\n\nimport _ \"example.com/svc/internal/store\"\n\nfunc main() {}\n")
	write("svc/internal/store/store.go", "package store\n")
	got, err := Imports(root, []string{"svc/cmd/main.go"})
	if err != nil {
		t.Fatal(err)
	}
	check(t, got, "svc/cmd/main.go", "3 example.com/svc/internal/store -> svc/internal/store")
}

func TestGoListTimeoutFallsBack(t *testing.T) {
	defer func(d time.Duration) { goListTimeout = d }(goListTimeout)
	goListTimeout = time.Nanosecond
	root := t.TempDir()
	write := func(p, s string) {
		p = filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/m\n\ngo 1.22\n")
	write("cmd/main.go", "package main\n\nimport _ \"example.com/m/store\"\n\nfunc main() {}\n")
	write("store/store.go", "package store\n")
	got, err := Imports(root, []string{"cmd/main.go"})
	if err != nil {
		t.Fatal(err)
	}
	check(t, got, "cmd/main.go", "3 example.com/m/store -> store")
}

func TestPythonImports(t *testing.T) {
	root := fixture(t, "python")
	got, err := Imports(root, []string{"pkg/api/handlers.py"})
	if err != nil {
		t.Fatal(err)
	}
	check(t, got, "pkg/api/handlers.py",
		"5 os -> ",
		"5 pkg.store -> pkg/store.py",
		"6 pkg.store -> pkg/store.py",
		"7 .views -> pkg/api/views.py",
		"8 ..store -> pkg/store.py",
		"9 ..util -> pkg/util.py",
		"10 pkg.store -> pkg/store.py",
		"14 lib.core -> src/lib/core.py",
		"16 .missing -> pkg/api/missing")
}

func TestPythonImportsTokenLines(t *testing.T) {
	root := fixture(t, "python")
	src := "from pkg import (\n" +
		"    store,\n" +
		"    util as u,\n" +
		"    NAME,\n" +
		")\n" +
		"from pkg.api import \\\n" +
		"    views\n"
	got, err := ImportsWith(root, []string{"pkg/new.py"}, func(string) ([]byte, bool, error) {
		return []byte(src), true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	check(t, got, "pkg/new.py",
		"2 pkg.store -> pkg/store.py",
		"3 pkg.util -> pkg/util.py",
		"1 pkg -> pkg",
		"7 pkg.api.views -> pkg/api/views.py")
}

func TestPythonImportsStringEscapes(t *testing.T) {
	root := fixture(t, "python")
	src := "x = 'it\\'s (' ; import os\n" + // escaped quote must not end the string
		"y = \"a\\\"(\"\n" + // escaped quote with a bracket after it
		"import pkg.store\n" +
		"z = \"\"\"doc \\\"\"\" still\n" +
		"import fake_in_docstring\n" +
		"\"\"\"\n" +
		"s = 'abc\\\n" + // single-quoted string continued by backslash
		"import fake_continued'\n" +
		"import pkg.util\n"
	got, err := ImportsWith(root, []string{"pkg/new.py"}, func(string) ([]byte, bool, error) {
		return []byte(src), true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	check(t, got, "pkg/new.py",
		"1 os -> ",
		"3 pkg.store -> pkg/store.py",
		"9 pkg.util -> pkg/util.py")
}

func TestJSImports(t *testing.T) {
	root := fixture(t, "ts")
	got, err := Imports(root, []string{"src/app.ts"})
	if err != nil {
		t.Fatal(err)
	}
	check(t, got, "src/app.ts",
		"1 ./store -> src/store.ts",
		"2 ./store -> src/store.ts",
		"6 ./lib -> src/lib/index.ts",
		"7 ./lib -> src/lib/index.ts",
		"8 react -> ",
		"11 ../shared/util.js -> shared/util.js",
		"12 ./lazy.js -> src/lazy.ts")
}

func TestImportsWithReadsGivenContent(t *testing.T) {
	read := func(p string) ([]byte, bool, error) {
		return []byte("import x from \"./y\";\n"), true, nil
	}
	got, err := ImportsWith(t.TempDir(), []string{"web/a.js"}, read)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]Import{"web/a.js": {{Path: "web/y", Raw: "./y", Line: 1}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
}

func TestResolveJSEscapingRootIsExternal(t *testing.T) {
	if p := resolveJS(t.TempDir(), "a.ts", "../outside"); p != "" {
		t.Fatalf("got %q", p)
	}
}
