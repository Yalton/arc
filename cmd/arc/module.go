package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Yalton/arc/internal/registry"
)

// repeatList is a repeatable flag whose values are kept whole (globs may
// contain commas).
type repeatList []string

func (s *repeatList) String() string { return strings.Join(*s, ",") }
func (s *repeatList) Set(v string) error {
	*s = append(*s, v)
	return nil
}

// runModule dispatches arc module set|rm|list (SPEC.md, "arc module").
func runModule(out *output, args []string) int {
	if len(args) == 0 {
		return out.fail(exitUsage, "module: need a subcommand: set, rm or list")
	}
	switch args[0] {
	case "set":
		return runModuleSet(out, args[1:])
	case "rm":
		return runModuleRm(out, args[1:])
	case "list":
		return runModuleList(out, args[1:])
	}
	return out.fail(exitUsage, fmt.Sprintf("module: unknown subcommand %q: use set, rm or list", args[0]))
}

func runModuleList(out *output, args []string) int {
	if len(args) > 0 {
		return out.fail(exitUsage, "module list: takes no arguments")
	}
	r, err := openRepo()
	if err != nil {
		return out.fail(exitUsage, err.Error())
	}
	mods := r.reg.Modules
	if mods == nil {
		mods = []registry.Module{}
	}
	var lines []string
	for _, m := range mods {
		lines = append(lines, m.ID+" — "+m.Summary)
	}
	if len(lines) == 0 {
		lines = append(lines, "module: none registered")
	}
	out.result(strings.Join(lines, "\n"), map[string]any{"modules": mods})
	return exitOK
}

func runModuleSet(out *output, args []string) int {
	fs := flag.NewFlagSet("module set", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var m registry.Module
	var paths, covers, entry, rules repeatList
	fs.StringVar(&m.Summary, "summary", "", "what the module is for (required)")
	fs.Var(&paths, "path", "glob of the module's files (repeatable)")
	fs.Var(&covers, "covers", "word or phrase the module covers (repeatable)")
	fs.Var(&entry, "entry", "public entry point (repeatable)")
	fs.StringVar(&m.Import, "import", "", "import path, for code outside the repo")
	fs.Var(&rules, "rule", "usage rule (repeatable)")
	fs.StringVar(&m.Source, "source", "", "where the module came from")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return out.fail(exitUsage, "module set: "+err.Error())
	}
	if len(pos) != 1 || pos[0] == "" {
		return out.fail(exitUsage, "module set: need exactly one module id")
	}
	if m.Summary == "" {
		return out.fail(exitUsage, "module set: --summary is required")
	}
	m.ID, m.Paths, m.Covers, m.Entry, m.Rules = pos[0], paths, covers, entry, rules

	return editRegistry(out, "module set", func(data []byte) ([]byte, bool, error) {
		b, err := registry.SetModule(data, m)
		return b, true, err
	}, func(changed []string) (string, any) {
		return "module set: " + m.ID + renderNote(changed), map[string]any{"module": m}
	}, nil)
}

func runModuleRm(out *output, args []string) int {
	fs := flag.NewFlagSet("module rm", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return out.fail(exitUsage, "module rm: "+err.Error())
	}
	if len(pos) != 1 || pos[0] == "" {
		return out.fail(exitUsage, "module rm: need exactly one module id")
	}
	id := pos[0]
	existed := false
	return editRegistry(out, "module rm", func(data []byte) ([]byte, bool, error) {
		b, ok, err := registry.RemoveModule(data, id)
		existed = ok
		return b, ok, err
	}, func(changed []string) (string, any) {
		return "module rm: removed " + id + renderNote(changed),
			map[string]any{"removed": id, "existed": true}
	}, func() (string, any) {
		return "module rm: " + id + " is not in the registry; nothing to remove",
			map[string]any{"removed": id, "existed": existed}
	})
}

func renderNote(changed []string) string {
	if len(changed) == 0 {
		return ""
	}
	return "\nrender: updated " + strings.Join(changed, ", ")
}

// editRegistry applies edit to the registry file, validates the result and
// renders. When validation fails it restores the previous bytes, prints the
// problems and exits 1. When edit reports no change nothing is written and
// unchanged (if given) is printed.
func editRegistry(out *output, name string,
	edit func([]byte) ([]byte, bool, error),
	done func(changed []string) (string, any),
	unchanged func() (string, any)) int {
	r, err := openRepo()
	if err != nil {
		return out.fail(exitUsage, err.Error())
	}
	path := filepath.Join(r.root, filepath.FromSlash(registry.Path))
	old, err := os.ReadFile(path)
	if err != nil {
		return out.fail(exitUsage, err.Error())
	}
	data, changed, err := edit(old)
	if err != nil {
		return out.fail(exitUsage, name+": "+err.Error())
	}
	if !changed && unchanged != nil {
		text, v := unchanged()
		out.result(text, v)
		return exitOK
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return out.fail(exitUsage, name+": "+err.Error())
	}
	restore := func() { _ = os.WriteFile(path, old, 0o644) }

	nr, err := openRepo()
	if err != nil {
		restore()
		return out.fail(exitUsage, name+": "+err.Error())
	}
	if problems := nr.reg.Validate(nr.files); len(problems) > 0 {
		restore()
		registry.SortProblems(problems)
		var b strings.Builder
		for _, p := range problems {
			fmt.Fprintf(&b, "%s: %s\n", registry.Path, p)
		}
		fmt.Fprintf(&b, "%s: %d problem(s); registry left unchanged", name, len(problems))
		out.result(b.String(), map[string]any{"problems": problems})
		return exitViolations
	}
	rendered, err := nr.reg.Render(nr.root)
	if err != nil {
		return out.fail(exitUsage, name+": render: "+err.Error())
	}
	text, v := done(rendered)
	out.result(text, v)
	return exitOK
}
