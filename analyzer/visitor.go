package analyzer

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

// Visitor visits the code nodes looking for variables that need to be closed
type Visitor struct {
	pass *analysis.Pass
}

// Do performs the visits to the code nodes
func (v *Visitor) Do(node ast.Node) bool {
	switch x := node.(type) {
	case *ast.CallExpr:
		//_ = ast.Print(fset, x.Fun)

		hasCloser(v.pass, closerType, x)

		selexpr, ok := x.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}

		//ident, ok := selexpr.X.(*ast.Ident)
		//if !ok {
		//return true
		//}

		//pos := fset.Position(selexpr.Sel.Pos())

		fn, ok := v.pass.TypesInfo.ObjectOf(selexpr.Sel).(*types.Func)

		if ok {
			println("<<<<<", fn.String())
		}
	case *ast.Ident:
		//fn := pkg.TypesInfo.ObjectOf(x) //.(*types.Func)

		//if fn != nil {
		//println(">>>>>", fn.String())
		//}
	}

	return true
}
