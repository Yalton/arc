package imports

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// goImports parses each file's import block for raw paths and lines, and
// resolves them with what `go list` reports for the files' modules.
func goImports(root string, files []string, read ReadFunc, out map[string][]Import) error {
	res := newGoResolver(root, files)
	for _, f := range files {
		data, ok, err := read(f)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		fset := token.NewFileSet()
		// On a syntax error the parser still returns the imports it read.
		af, _ := parser.ParseFile(fset, f, data, parser.ImportsOnly)
		if af == nil {
			continue
		}
		imps := []Import{}
		for _, spec := range af.Imports {
			raw, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				continue
			}
			imps = append(imps, Import{Path: res.resolve(raw), Raw: raw, Line: fset.Position(spec.Path.Pos()).Line})
		}
		out[f] = imps
	}
	return nil
}

// goResolver maps Go import paths to repo-relative package dirs.
type goResolver struct {
	roots []string          // absolute root, and its symlink-free form
	pkgs  map[string]string // import path -> dir, from go list
	mods  map[string]string // module path -> dir, from go list and go.mod
}

func newGoResolver(root string, files []string) *goResolver {
	r := &goResolver{pkgs: map[string]string{}, mods: map[string]string{}}
	if abs, err := filepath.Abs(root); err == nil {
		r.roots = append(r.roots, abs)
		if real, err := filepath.EvalSymlinks(abs); err == nil && real != abs {
			r.roots = append(r.roots, real)
		}
	}
	// Group package dirs by their nearest module and list each module once.
	byMod := map[string][]string{}
	seen := map[string]bool{}
	for _, f := range files {
		dir := path.Dir(f)
		mod, ok := findModule(root, dir)
		if !ok || seen[dir] {
			continue
		}
		seen[dir] = true
		if mp := modulePath(filepath.Join(root, filepath.FromSlash(mod), "go.mod")); mp != "" {
			r.mods[mp] = mod
		}
		pat := "."
		switch {
		case mod == "." && dir != ".":
			pat = "./" + dir
		case dir != mod:
			pat = "./" + strings.TrimPrefix(dir, mod+"/")
		}
		byMod[mod] = append(byMod[mod], pat)
	}
	mods := make([]string, 0, len(byMod))
	for m := range byMod {
		mods = append(mods, m)
	}
	sort.Strings(mods)
	for _, m := range mods {
		r.list(filepath.Join(root, filepath.FromSlash(m)), byMod[m])
	}
	return r
}

// goListTimeout bounds one go list run; on expiry the go.mod fallback
// applies as for any other go list failure. goListWaitDelay bounds the wait
// for its output pipes after the process is killed.
var (
	goListTimeout   = 2 * time.Minute
	goListWaitDelay = 5 * time.Second
)

// list runs go list on the package patterns in module dir and records every
// in-repo package and module it reports, including test imports and
// dependencies. Failures are tolerated: whatever was printed is used, and
// the go.mod module path remains as a fallback. The network and toolchain
// downloads are off so the gate stays deterministic.
func (r *goResolver) list(dir string, pats []string) {
	ctx, cancel := context.WithTimeout(context.Background(), goListTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", append([]string{"list", "-e", "-json", "-deps", "-test"}, pats...)...)
	cmd.WaitDelay = goListWaitDelay
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOPROXY=off", "GOTOOLCHAIN=local")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	_ = cmd.Run()
	dec := json.NewDecoder(&stdout)
	for {
		var p struct {
			ImportPath string
			Dir        string
			Standard   bool
			Module     *struct{ Path, Dir string }
		}
		if dec.Decode(&p) != nil {
			return
		}
		if p.Module != nil && p.Module.Dir != "" {
			if rel, ok := r.rel(p.Module.Dir); ok {
				r.mods[p.Module.Path] = rel
			}
		}
		if p.Standard || p.Dir == "" {
			continue
		}
		ip, _, _ := strings.Cut(p.ImportPath, " ") // "pkg [pkg.test]" variants
		if rel, ok := r.rel(p.Dir); ok {
			r.pkgs[ip] = rel
		}
	}
}

// rel returns dir relative to the repository root, if it is inside it.
func (r *goResolver) rel(dir string) (string, bool) {
	for _, root := range r.roots {
		if rel, err := filepath.Rel(root, dir); err == nil {
			if rel, ok := inRepo(filepath.ToSlash(rel)); ok {
				return rel, true
			}
		}
	}
	return "", false
}

// resolve returns the repo-relative dir for import path ip: the package go
// list reported, else the path under the longest in-repo module prefix
// (covers packages go list did not reach, such as build-tagged imports or
// packages that do not exist yet); "" if external.
func (r *goResolver) resolve(ip string) string {
	if d, ok := r.pkgs[ip]; ok {
		return d
	}
	best, dir := "", ""
	for mp, d := range r.mods {
		if len(mp) <= len(best) {
			continue
		}
		switch {
		case ip == mp:
			best, dir = mp, d
		case strings.HasPrefix(ip, mp+"/"):
			best, dir = mp, path.Join(d, ip[len(mp)+1:])
		}
	}
	return dir
}

// findModule returns the repo-relative dir of the nearest go.mod at or above
// dir, staying inside the repository.
func findModule(root, dir string) (string, bool) {
	for {
		if isFile(root, path.Join(dir, "go.mod")) {
			return dir, true
		}
		if dir == "." || dir == "/" || dir == "" {
			return "", false
		}
		dir = path.Dir(dir)
	}
}

// modulePath reads the module directive of a go.mod file.
func modulePath(gomod string) string {
	data, err := os.ReadFile(gomod)
	if err != nil {
		return ""
	}
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) >= 2 && f[0] == "module" {
			if s, err := strconv.Unquote(f[1]); err == nil {
				return s
			}
			return f[1]
		}
	}
	return ""
}
