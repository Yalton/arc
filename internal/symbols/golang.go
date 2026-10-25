package symbols

import (
	"go/ast"
	"go/parser"
	"go/token"
)

// goSymbols returns the exported top-level funcs, types, consts and vars
// of a Go file, and exported methods of exported types as Type.Method.
// A parse error keeps whatever declarations were read before it.
func goSymbols(file string, data []byte) []Symbol {
	fset := token.NewFileSet()
	f, _ := parser.ParseFile(fset, file, data, parser.ParseComments|parser.SkipObjectResolution)
	if f == nil {
		return nil
	}
	var out []Symbol
	add := func(name *ast.Ident, kind string, docs ...*ast.CommentGroup) {
		s := Symbol{Name: name.Name, Kind: kind, File: file, Line: fset.Position(name.Pos()).Line}
		for _, d := range docs {
			if d != nil {
				s.Doc = firstSentence(d.Text())
				break
			}
		}
		out = append(out, s)
	}
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			if !d.Name.IsExported() {
				continue
			}
			if d.Recv == nil {
				add(d.Name, KindFunc, d.Doc)
				continue
			}
			recv := receiverType(d.Recv)
			if recv == nil || !recv.IsExported() {
				continue
			}
			add(&ast.Ident{Name: recv.Name + "." + d.Name.Name, NamePos: d.Name.Pos()}, KindMethod, d.Doc)
		case *ast.GenDecl:
			// A group's doc describes its only spec, not each of several.
			group := d.Doc
			if len(d.Specs) > 1 {
				group = nil
			}
			for _, sp := range d.Specs {
				switch sp := sp.(type) {
				case *ast.TypeSpec:
					if sp.Name.IsExported() {
						add(sp.Name, KindType, sp.Doc, group)
					}
				case *ast.ValueSpec:
					kind := KindVar
					if d.Tok == token.CONST {
						kind = KindConst
					}
					for _, n := range sp.Names {
						if n.IsExported() {
							add(n, kind, sp.Doc, group, sp.Comment)
						}
					}
				}
			}
		}
	}
	return out
}

// receiverType returns the receiver's type name, through pointers and
// type parameters.
func receiverType(fl *ast.FieldList) *ast.Ident {
	if len(fl.List) == 0 {
		return nil
	}
	t := fl.List[0].Type
	for {
		switch x := t.(type) {
		case *ast.StarExpr:
			t = x.X
		case *ast.IndexExpr:
			t = x.X
		case *ast.IndexListExpr:
			t = x.X
		case *ast.ParenExpr:
			t = x.X
		case *ast.Ident:
			return x
		default:
			return nil
		}
	}
}
