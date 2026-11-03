package gate

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/Yalton/arc/internal/gitx"
	"github.com/Yalton/arc/internal/registry"
)

// maxWiringFile is the size above which a file is not searched for
// references to a module's entry.
const maxWiringFile = 1 << 20

// goListTimeout bounds each go list run of the wiring check.
var goListTimeout = 2 * time.Minute

// checkModules protects installed modules (SPEC.md, "arc gate"): files
// changed under a module that existed at the comparison point, dirty
// submodule work trees, go.mod replacing a module's import, and modules
// nothing outside module paths references.
func checkModules(ctx *Context) ([]Finding, error) {
	if len(ctx.Reg.Modules) == 0 && (ctx.BaseReg == nil || len(ctx.BaseReg.Modules) == 0) {
		return nil, nil
	}
	links, err := gitx.Gitlinks(ctx.Root)
	if err != nil {
		return nil, err
	}
	out := changedModuleFiles(ctx, links)
	out = append(out, dirtySubmodules(ctx, links)...)
	repl, err := replacedImports(ctx)
	if err != nil {
		return nil, err
	}
	out = append(out, repl...)
	unwired, err := unwiredModules(ctx)
	if err != nil {
		return nil, err
	}
	return append(out, unwired...), nil
}

// changedModuleFiles reports files modified or deleted under a module of
// the comparison point's registry. A module new in this change is being
// installed, a moved gitlink is not an edit, and added files are left out
// because tools generate files beside installed ones (Godot's *.uid and
// *.import); the hook still denies new files there. --all has no change set.
func changedModuleFiles(ctx *Context, links []string) []Finding {
	if ctx.All || ctx.BaseReg == nil || len(ctx.BaseReg.Modules) == 0 {
		return nil
	}
	if ctx.Approved {
		return []Finding{approvedSkip()}
	}
	gitlink := map[string]bool{}
	for _, l := range links {
		gitlink[l] = true
	}
	var out []Finding
	for _, fc := range ctx.Changes.Files {
		m, ok := ctx.BaseReg.ModuleOf(fc.Path)
		if !ok || gitlink[fc.Path] || fc.Status == gitx.Added {
			continue
		}
		out = append(out, Finding{Path: fc.Path, Entry: m.ID,
			Message: "module " + m.ID + ": file changed under module path"})
	}
	return out
}

// dirtySubmodules reports checked-out submodules under module paths whose
// work tree has modified tracked files (untracked files are generated ones,
// as for added files in changedModuleFiles). A submodule whose status
// fails is a warning, so one broken checkout does not stop the gate.
func dirtySubmodules(ctx *Context, links []string) []Finding {
	var out []Finding
	for _, l := range links {
		m, ok := ctx.Reg.ModuleOf(l)
		if !ok {
			continue
		}
		dir := filepath.Join(ctx.Root, filepath.FromSlash(l))
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err != nil {
			continue // not checked out
		}
		st, err := gitx.SubmoduleModified(dir)
		if err != nil {
			out = append(out, Finding{Severity: Warning, Path: l, Entry: m.ID,
				Message: "module " + m.ID + ": submodule status not checked (" + strings.TrimSpace(err.Error()) + ")"})
			continue
		}
		if strings.TrimSpace(st) != "" {
			out = append(out, Finding{Path: l, Entry: m.ID,
				Message: "module " + m.ID + ": " + registry.SubmoduleDirty})
		}
	}
	return out
}

// replacedImports reports tracked go.mod files outside module paths whose
// replace directives redirect a module's import (or a prefix of it at a /).
func replacedImports(ctx *Context) ([]Finding, error) {
	if !slices.ContainsFunc(ctx.Reg.Modules, func(m registry.Module) bool { return m.Import != "" }) {
		return nil, nil
	}
	tracked, err := gitx.TrackedFiles(ctx.Root)
	if err != nil {
		return nil, err
	}
	var out []Finding
	for _, f := range tracked {
		if f != "go.mod" && !strings.HasSuffix(f, "/go.mod") {
			continue
		}
		if _, ok := ctx.Reg.ModuleOf(f); ok {
			continue
		}
		data, ok, err := ctx.ReadFile(f)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		reported := map[string]bool{}
		for _, old := range replaceSources(data) {
			for _, m := range ctx.Reg.Modules {
				if m.Import != "" && !reported[m.ID] && (m.Import == old || strings.HasPrefix(m.Import, old+"/")) {
					reported[m.ID] = true
					out = append(out, Finding{Path: f, Entry: m.ID,
						Message: "module " + m.ID + ": go.mod replaces " + m.Import})
				}
			}
		}
	}
	return out, nil
}

// replaceSources returns the left-hand module paths of go.mod's replace
// directives, single or in a block, spaced or not ("replace(", "a=>b").
func replaceSources(gomod []byte) []string {
	pad := strings.NewReplacer("(", " ( ", ")", " ) ", "=>", " => ")
	var out []string
	inBlock := false
	for _, line := range strings.Split(string(gomod), "\n") {
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		fields := strings.Fields(pad.Replace(line))
		if !inBlock {
			if len(fields) == 0 || fields[0] != "replace" {
				continue
			}
			fields = fields[1:]
			if len(fields) > 0 && fields[0] == "(" {
				inBlock, fields = true, fields[1:]
			}
		}
		if inBlock && len(fields) > 0 && fields[len(fields)-1] == ")" {
			inBlock, fields = false, fields[:len(fields)-1]
		}
		if len(fields) > 1 && slices.Contains(fields, "=>") {
			out = append(out, strings.Trim(fields[0], `"`))
		}
	}
	return out
}

// unwiredModules reports modules that nothing outside module paths uses:
// for a module with an import, no main package depends on it (go list
// alone: a Go entry's package name is in go.mod and go.sum once the module
// is installed); for one without, no entry's leading identifier occurs as
// a whole word in a file that wiring searches.
// With --staged, a module absent from the HEAD registry is being installed
// by this commit and is not judged, so an install commit passes pre-commit.
func unwiredModules(ctx *Context) ([]Finding, error) {
	if len(ctx.Reg.Modules) == 0 {
		return nil, nil
	}
	referenced, err := entryReferences(ctx)
	if err != nil {
		return nil, err
	}
	var deps map[string]bool
	var depsErr error
	listed := false
	var out []Finding
	for _, m := range ctx.Reg.Modules {
		if referenced[m.ID] || installing(ctx, m.ID) {
			continue
		}
		if m.Import == "" {
			out = append(out, Finding{Entry: m.ID, Message: "module " + m.ID +
				": not wired (no reference to " + m.Surface() + " outside module paths)"})
			continue
		}
		if !listed {
			deps, depsErr = mainDeps(ctx.Root)
			listed = true
		}
		if depsErr != nil {
			out = append(out, Finding{Severity: Warning, Entry: m.ID,
				Message: "module " + m.ID + ": wiring not checked (go list failed)"})
			continue
		}
		if !importListed(deps, m.Import) {
			out = append(out, Finding{Entry: m.ID, Message: "module " + m.ID +
				": not wired (no main package depends on " + m.Import + ")"})
		}
	}
	return out, nil
}

// installing reports whether --staged judges a commit that adds module id.
func installing(ctx *Context, id string) bool {
	if !ctx.Staged {
		return false
	}
	if ctx.BaseReg != nil {
		for _, m := range ctx.BaseReg.Modules {
			if m.ID == id {
				return false
			}
		}
	}
	return true
}

// entryPattern is one module's wiring text match: the literal leading
// identifiers, every one of which a regexp match contains, and the
// whole-word regexp itself.
type entryPattern struct {
	idents [][]byte
	re     *regexp.Regexp
}

// match runs the regexp only on text that contains an identifier, which
// most files do not; bytes.Contains is far faster than the regexp.
func (p entryPattern) match(data []byte) bool {
	for _, id := range p.idents {
		if bytes.Contains(data, id) {
			return p.re.Match(data)
		}
	}
	return false
}

// entryReferences returns the ids of modules without an import whose entry
// (by its leading identifier, which every full entry match also contains)
// occurs as a whole word in a file that wiring searches.
func entryReferences(ctx *Context) (map[string]bool, error) {
	pats := map[string]entryPattern{}
	for _, m := range ctx.Reg.Modules {
		if m.Import != "" {
			continue
		}
		var p entryPattern
		var alts []string
		for _, e := range m.Entry {
			if e != "" {
				id := leadingIdent(e)
				p.idents = append(p.idents, []byte(id))
				alts = append(alts, wordPattern(id))
			}
		}
		if len(alts) > 0 {
			p.re = regexp.MustCompile(strings.Join(alts, "|"))
			pats[m.ID] = p
		}
	}
	found := map[string]bool{}
	if len(pats) == 0 {
		return found, nil
	}
	for _, f := range ctx.Files {
		if len(found) == len(pats) {
			break
		}
		data, err := wiringContent(ctx, f)
		if err != nil {
			return nil, err
		}
		for id, p := range pats {
			if !found[id] && p.match(data) {
				found[id] = true
			}
		}
	}
	return found, nil
}

// goModFiles name installed modules, not uses of them, so wiring does not
// search them.
var goModFiles = map[string]bool{"go.mod": true, "go.sum": true, "go.work": true, "go.work.sum": true}

// binarySniff is how much of a file wiring reads to decide it is binary.
const binarySniff = 8 << 10

// wiringContent returns f's working-tree content when wiring searches it,
// else nil: ignored files, module paths, Markdown, go.mod and friends,
// binary files (a NUL in the first 8 KiB, read before the rest) and files
// over 1 MiB are left out.
func wiringContent(ctx *Context, f string) ([]byte, error) {
	if ctx.Reg.IsIgnored(f) || strings.HasSuffix(strings.ToLower(f), ".md") || goModFiles[path.Base(f)] {
		return nil, nil
	}
	if _, ok := ctx.Reg.ModuleOf(f); ok {
		return nil, nil
	}
	p := filepath.Join(ctx.Root, filepath.FromSlash(f))
	fi, err := os.Lstat(p)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() > maxWiringFile {
		return nil, nil
	}
	return readText(p, fi.Size())
}

// readText returns the content of the regular file p, or nil when its
// first binarySniff bytes hold a NUL; a file that vanished is nil too.
func readText(p string, size int64) ([]byte, error) {
	fh, err := os.Open(p)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	data := make([]byte, 0, size+1)
	n, err := io.ReadFull(fh, data[:min(int64(binarySniff), size+1)])
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return nil, err
	}
	data = data[:n]
	if bytes.IndexByte(data, 0) >= 0 {
		return nil, nil
	}
	rest, err := io.ReadAll(fh)
	if err != nil {
		return nil, err
	}
	return append(data, rest...), nil
}

// leadingIdent is the text of an entry before its first ".", or the whole
// entry when there is none or that text is empty (".X").
func leadingIdent(entry string) string {
	if i := strings.IndexByte(entry, '.'); i > 0 {
		return entry[:i]
	}
	return entry
}

// wordPattern matches s as a whole word: \b on a side whose edge character
// is a word character, else a non-word character or the text's edge, so
// "$Kit" and "@scope/pkg" still match.
func wordPattern(s string) string {
	isWord := func(c byte) bool {
		return c == '_' || '0' <= c && c <= '9' || 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z'
	}
	before, after := `(?:^|[^\w])`, `(?:[^\w]|$)`
	if isWord(s[0]) {
		before = `\b`
	}
	if isWord(s[len(s)-1]) {
		after = `\b`
	}
	return `(?:` + before + regexp.QuoteMeta(s) + after + `)`
}

// mainDeps returns the import paths go list -deps reports for the
// repository's main packages, with network and toolchain downloads off.
func mainDeps(root string) (map[string]bool, error) {
	mains, err := goList(root, "-f", `{{if eq .Name "main"}}{{.ImportPath}}{{end}}`, "./...")
	if err != nil {
		return nil, err
	}
	deps := map[string]bool{}
	if len(mains) == 0 {
		return deps, nil
	}
	pkgs, err := goList(root, append([]string{"-deps"}, mains...)...)
	if err != nil {
		return nil, err
	}
	for _, p := range pkgs {
		deps[p] = true
	}
	return deps, nil
}

func goList(root string, args ...string) ([]string, error) {
	c, cancel := context.WithTimeout(context.Background(), goListTimeout)
	defer cancel()
	cmd := exec.CommandContext(c, "go", append([]string{"list"}, args...)...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOPROXY=off", "GOTOOLCHAIN=local")
	cmd.WaitDelay = 5 * time.Second
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return strings.Fields(string(out)), nil
}

// importListed reports whether deps lists imp or a package under imp/.
func importListed(deps map[string]bool, imp string) bool {
	for p := range deps {
		if p == imp || strings.HasPrefix(p, imp+"/") {
			return true
		}
	}
	return false
}
