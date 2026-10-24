package gate

import (
	"fmt"
	"strings"

	"arc/internal/gitx"
	"arc/internal/imports"
	"arc/internal/registry"
)

// checkBoundaries fails imports in changed files that cross a boundary: the
// file matches the boundary's from globs and the import resolves to a path
// matching its deny globs. Only imports on added lines count, so crossings
// that already existed in a touched file pass; with --all every line is
// added and the whole tree is judged.
func checkBoundaries(ctx *Context) ([]Finding, error) {
	if len(ctx.Reg.Boundaries) == 0 {
		return nil, nil
	}
	var files []string
	for _, fc := range ctx.Changes.Files {
		if fc.Status != gitx.Deleted && len(fromBoundaries(ctx.Reg, fc.Path)) > 0 {
			files = append(files, fc.Path)
		}
	}
	if len(files) == 0 {
		return nil, nil
	}
	imps, err := imports.ImportsWith(ctx.Root, files, ctx.ReadFile)
	if err != nil {
		return nil, err
	}
	under := dirIndex(ctx.Files)

	var out []Finding
	for _, f := range files {
		added := map[int]bool{}
		for _, ln := range ctx.Changes.File(f).Added {
			added[ln.Num] = true
		}
		for _, imp := range imps[f] {
			if imp.Path == "" || !added[imp.Line] {
				continue
			}
			for _, b := range fromBoundaries(ctx.Reg, f) {
				if !denied(b.Deny, imp.Path, under) {
					continue
				}
				out = append(out, Finding{
					Path:  f,
					Line:  imp.Line,
					Entry: fmt.Sprintf("boundaries[%s]", b.ID),
					Message: fmt.Sprintf("imports %s (%s), crossing boundary %s: %s. Route it through the allowed layer or change the registry with a reason.",
						imp.Raw, imp.Path, b.ID, strings.TrimRight(strings.TrimSpace(b.Reason), ".")),
				})
			}
		}
	}
	return out, nil
}

// fromBoundaries returns the boundaries whose from globs match path.
func fromBoundaries(reg *registry.Registry, path string) []registry.Boundary {
	var out []registry.Boundary
	for _, b := range reg.Boundaries {
		for _, g := range b.From {
			if registry.Match(g, path) {
				out = append(out, b)
				break
			}
		}
	}
	return out
}

// dirIndex maps every directory to the repository files below it.
func dirIndex(files []string) map[string][]string {
	idx := map[string][]string{}
	for _, f := range files {
		for d := f; ; {
			i := strings.LastIndex(d, "/")
			if i < 0 {
				break
			}
			d = d[:i]
			idx[d] = append(idx[d], f)
		}
	}
	return idx
}

// denied reports whether a resolved import path matches a deny glob. A
// package dir matches when the dir itself, a hypothetical file in it, or any
// repository file below it matches.
func denied(deny []string, p string, under map[string][]string) bool {
	for _, g := range deny {
		if registry.Match(g, p) || registry.Match(g, p+"/x") {
			return true
		}
		for _, f := range under[p] {
			if registry.Match(g, f) {
				return true
			}
		}
	}
	return false
}
