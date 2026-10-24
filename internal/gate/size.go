package gate

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"

	"arc/internal/gitx"
)

// checkSize fails changed files over limits.file_lines and, for Go, changed
// functions over limits.function_lines. A function counts as changed when
// an added line falls inside it, so untouched long functions pass unless
// the whole tree is judged. Ignored files are exempt. Contents come from
// ctx.ReadFile (the index for --staged).
func checkSize(ctx *Context) ([]Finding, error) {
	lim := ctx.Reg.Limits
	if lim.FileLines <= 0 && lim.FunctionLines <= 0 {
		return nil, nil
	}
	var out []Finding
	for _, fc := range ctx.Changes.Files {
		if fc.Status == gitx.Deleted || ctx.Reg.IsIgnored(fc.Path) {
			continue
		}
		data, ok, err := ctx.ReadFile(fc.Path)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		if bytes.IndexByte(data, 0) >= 0 {
			continue // binary
		}
		if lim.FileLines > 0 {
			if n := countLines(data); n > lim.FileLines {
				out = append(out, Finding{Path: fc.Path, Entry: "limits.file_lines", Message: fmt.Sprintf(
					"%d lines, over the limit of %d. Split it along the concept's responsibilities.", n, lim.FileLines)})
			}
		}
		if lim.FunctionLines > 0 && strings.HasSuffix(fc.Path, ".go") {
			out = append(out, longFuncs(fc, data, lim.FunctionLines)...)
		}
	}
	return out, nil
}

func countLines(data []byte) int {
	if len(data) == 0 {
		return 0
	}
	n := bytes.Count(data, []byte{'\n'})
	if data[len(data)-1] != '\n' {
		n++
	}
	return n
}

// longFuncs reports Go functions longer than limit (func keyword to closing
// brace) that contain an added line. Unparseable files are skipped.
func longFuncs(fc gitx.FileChange, data []byte, limit int) []Finding {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, fc.Path, data, parser.SkipObjectResolution)
	if err != nil {
		return nil
	}
	var out []Finding
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		start := fset.Position(fn.Pos()).Line
		end := fset.Position(fn.End()).Line
		n := end - start + 1
		if n <= limit || !touched(fc.Added, start, end) {
			continue
		}
		out = append(out, Finding{Path: fc.Path, Line: start, Entry: "limits.function_lines", Message: fmt.Sprintf(
			"func %s is %d lines, over the limit of %d. Extract helpers.", funcName(fn), n, limit)})
	}
	return out
}

func touched(added []gitx.Line, start, end int) bool {
	for _, l := range added {
		if l.Num >= start && l.Num <= end {
			return true
		}
	}
	return false
}

func funcName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name
	}
	t := fn.Recv.List[0].Type
	if s, ok := t.(*ast.StarExpr); ok {
		t = s.X
	}
	if g, ok := t.(*ast.IndexExpr); ok {
		t = g.X
	}
	if g, ok := t.(*ast.IndexListExpr); ok {
		t = g.X
	}
	if id, ok := t.(*ast.Ident); ok {
		return "(" + id.Name + ")." + fn.Name.Name
	}
	return fn.Name.Name
}
