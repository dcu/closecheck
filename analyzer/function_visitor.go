package analyzer

import (
	"fmt"
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

// FunctionVisitor is in charge of preprocessing packages to find functions that close io.Closers
type FunctionVisitor struct {
	pass *analysis.Pass
}

type isCloser struct {
}

type ioCloserFunc struct {
	obj                *types.Func
	fdecl              *ast.FuncDecl
	argsThatAreClosers []bool
	argNames           []*ast.Ident
	isCloser           bool
}

func (c *isCloser) AFact() {}

func (pp *FunctionVisitor) debug(n ast.Node) {
	_ = ast.Print(pp.pass.Fset, n)
}

// this function finds functions that receive and closes an io.Closer
func (pp *FunctionVisitor) findFunctionsThatReceiveAnIOCloser() map[*types.Func]*ioCloserFunc {
	receivers := map[*types.Func]*ioCloserFunc{}

	for _, file := range pp.pass.Files {
		for _, decl := range file.Decls {
			fdecl, ok := decl.(*ast.FuncDecl)
			if !ok || fdecl.Body == nil {
				continue
			}

			fn, ok := pp.pass.TypesInfo.Defs[fdecl.Name].(*types.Func)
			if !ok {
				continue
			}

			sig := fn.Type().(*types.Signature)
			params := sig.Params()

			receivesCloser := false
			argsThatAreClosers := make([]bool, params.Len())
			argNames := []*ast.Ident{}

			if len(fdecl.Type.Params.List) > 0 {
				argNames = fdecl.Type.Params.List[0].Names
			}

			for i := 0; i < params.Len(); i++ {
				param := params.At(i)

				if types.Implements(param.Type(), closerType) {
					receivesCloser = true
					argsThatAreClosers[i] = true
				}
			}

			if receivesCloser {
				receivers[fn] = &ioCloserFunc{
					obj:                fn,
					fdecl:              fdecl,
					argsThatAreClosers: argsThatAreClosers,
					argNames:           argNames,
				}
			}
		}
	}

	for _, rcv := range receivers {
		fmt.Println(">>> ", rcv.obj.FullName())

		for _, id := range rcv.argNames {
			if pp.traverse(id, rcv.fdecl.Body.List) {
				println("FOOOOOOUND!")

				rcv.isCloser = true
			}
		}
	}

	return receivers
}

func (pp *FunctionVisitor) traverse(id *ast.Ident, stmts []ast.Stmt) bool {
	for _, stmt := range stmts {
		switch castedStmt := stmt.(type) {
		case *ast.ReturnStmt:
			fmt.Println("found on return!", castedStmt, id)
		case *ast.DeferStmt:
			fmt.Println("found on defer!", castedStmt, id)
		case *ast.ExprStmt:
			fmt.Println("found on expr!", castedStmt, id)

			if pp.closesIdentOnExpression(id, castedStmt.X) {
				return true
			}
		case *ast.AssignStmt:
			fmt.Println("found on assign!", castedStmt, id)

			if pp.closesIdentOnAnyExpression(id, castedStmt.Rhs) {
				return true
			}
		case *ast.BlockStmt:
			fmt.Println("found block, doing nothing for now", castedStmt, id)
		}
	}

	return false
}

func (pp *FunctionVisitor) closesIdentOnAnyExpression(id *ast.Ident, exprs []ast.Expr) bool {
	for _, expr := range exprs {
		if pp.closesIdentOnExpression(id, expr) {
			return true
		}
	}

	return false
}

func (pp *FunctionVisitor) closesIdentOnExpression(id *ast.Ident, expr ast.Expr) bool {
	switch castedExpr := expr.(type) {
	case *ast.CallExpr:
		if castedExpr.Fun != nil {
			return pp.closesIdentOnExpression(id, castedExpr.Fun)
		}

	case *ast.SelectorExpr:
		if pp.isExprEqualToIdent(id, castedExpr.X) && castedExpr.Sel.Name == "Close" {
			pp.debug(castedExpr)
			return true
		}
	}

	return false
}

func (pp *FunctionVisitor) isExprEqualToIdent(id *ast.Ident, x ast.Expr) bool {
	xIdent, ok := x.(*ast.Ident)
	if !ok {
		return false
	}

	if id.NamePos == xIdent.NamePos {
		return true
	}

	if xIdent.Obj == nil || xIdent.Obj.Decl == nil {
		return false
	}

	vdecl, ok := xIdent.Obj.Decl.(*ast.Field)
	if !ok || len(vdecl.Names) != 1 {
		return false
	}

	return vdecl.Names[0].NamePos == id.NamePos
}
