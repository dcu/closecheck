package analyzer

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"unicode"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"
)

var (
	enableFunctionDebugger   = false
	showCloserFunctionsFound = false
)

// FunctionVisitor is in charge of preprocessing packages to find functions that close io.Closers
type FunctionVisitor struct {
	pass            *analysis.Pass
	receivers       map[*types.Func]*ioCloserFunc
	localGlobalVars map[token.Pos]bool
}

type ioCloserFunc struct {
	obj                *types.Func
	fdecl              *ast.FuncDecl
	argsThatAreClosers []bool
	argNames           []*ast.Ident
	isCloser           bool
}

func (c *ioCloserFunc) AFact() {}

// String is the string representation of the fact
func (c *ioCloserFunc) String() string {
	if c.isCloser {
		return "is closer"
	}

	return "is not closer"
}

func (pp *FunctionVisitor) debug(n ast.Node, template string, args ...interface{}) {
	if !enableFunctionDebugger {
		return
	}

	fmt.Printf(template+"\n", args...)

	_ = ast.Print(pp.pass.Fset, n)
}

// this function finds functions that receive and closes an io.Closer
func (pp *FunctionVisitor) findFunctionsThatReceiveAnIOCloser() map[*types.Func]*ioCloserFunc {
	pp.receivers = map[*types.Func]*ioCloserFunc{}
	pp.localGlobalVars = map[token.Pos]bool{}

	for _, file := range pp.pass.Files {
		for _, decl := range file.Decls {
			switch cDecl := decl.(type) {
			case *ast.GenDecl:
				if cDecl.Tok != token.VAR {
					continue
				}

				for _, spec := range cDecl.Specs {
					varSpec, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}

					for _, name := range varSpec.Names {
						obj := pp.pass.TypesInfo.ObjectOf(name)
						if obj == nil {
							continue
						}

						if isCloserReceiver(obj.Type().Underlying()) {
							pp.localGlobalVars[name.NamePos] = true
						}
					}
				}
			case *ast.FuncDecl:
				if cDecl.Body == nil {
					continue
				}

				fn, ok := pp.pass.TypesInfo.Defs[cDecl.Name].(*types.Func)
				if !ok {
					continue
				}

				//if fn.Name() != "CloseWithDefer" && fn.Name() != "doClose" {
				//continue
				//}

				//_ = ast.Print(pp.pass.Fset, decl)

				sig := fn.Type().(*types.Signature)
				params := sig.Params()

				receivesCloser := false
				argsThatAreClosers := make([]bool, params.Len())
				argNames := []*ast.Ident{}

				for _, params := range cDecl.Type.Params.List {
					argNames = append(argNames, params.Names...) // FIXME: should only contain io.Closers
				}

				for i := 0; i < params.Len(); i++ {
					param := params.At(i)

					if isCloserReceiver(param.Type()) {
						receivesCloser = true
						argsThatAreClosers[i] = true
					}
				}

				if receivesCloser {
					pp.receivers[fn] = &ioCloserFunc{
						obj:                fn,
						fdecl:              cDecl,
						argsThatAreClosers: argsThatAreClosers,
						argNames:           argNames,
					}
				}
			}
		}
	}

	for _, rcv := range pp.receivers {
		for _, id := range rcv.argNames {
			if pp.traverse(id, rcv.fdecl.Body.List) {
				rcv.isCloser = true
			}
		}

		if showCloserFunctionsFound {
			fmt.Println("found closer function:", rcv.obj.FullName(), "closer:", rcv.isCloser, "pos:", rcv.obj.Pos())
		}
	}

	// TODO: optimize this, no need to loop again over all receivers
	for _, rcv := range pp.receivers {
		for _, id := range rcv.argNames {
			if pp.traverse(id, rcv.fdecl.Body.List) {
				rcv.isCloser = true
			}
		}

		if showCloserFunctionsFound {
			fmt.Println("found closer function:", rcv.obj.FullName(), "closer:", rcv.isCloser, "pos:", rcv.obj.Pos())
		}

		pp.pass.ExportObjectFact(rcv.obj, rcv)
	}

	return pp.receivers
}

func isCloserReceiver(t types.Type) bool {
	if types.Implements(t, closerType) {
		return true
	}

	// special case: a struct containing a io.Closer fields that implements io.Closer, like http.Response.Body
	ptr, ok := t.Underlying().(*types.Pointer)
	if !ok {
		return false
	}

	str, ok := ptr.Elem().Underlying().(*types.Struct)
	if !ok {
		return false
	}

	for i := 0; i < str.NumFields(); i++ {
		v := str.Field(i)
		fieldName := v.Name()

		// TODO: don't ignore unexported fields if the struct is in the current package
		if types.Implements(v.Type(), closerType) && unicode.IsUpper([]rune(fieldName)[0]) {
			return true
		}
	}

	return false
}

func (pp *FunctionVisitor) traverse(id *ast.Ident, stmts []ast.Stmt) bool {
	for _, stmt := range stmts {
		switch castedStmt := stmt.(type) {
		case *ast.IfStmt:
			pp.debug(castedStmt, "found if stmt")

			if pp.traverse(id, []ast.Stmt{castedStmt.Init}) {
				return true
			}

			if pp.traverse(id, castedStmt.Body.List) {
				return true
			}
		case *ast.ReturnStmt:
			pp.debug(castedStmt, "found return stmt")

			if pp.closesIdentOnAnyExpression(id, castedStmt.Results) {
				return true
			}

		case *ast.DeferStmt:
			pp.debug(castedStmt, "found defer stmt, checking id: %s", id.String())

			if pp.closesIdentOnExpression(id, castedStmt.Call) {
				return true
			}
		case *ast.ExprStmt:
			pp.debug(castedStmt, "found expr stmt")

			if pp.closesIdentOnExpression(id, castedStmt.X) {
				return true
			}

		case *ast.AssignStmt:
			pp.debug(castedStmt, "found assign stmt comparing against: %s", id.String())

			if pp.closesIdentOnAnyExpression(id, castedStmt.Rhs) {
				return true
			}

		case *ast.BlockStmt:
			pp.debug(castedStmt, "found block stmt")
		}
	}

	return false
}

func (pp *FunctionVisitor) findKnownReceiverFromCall(call *ast.CallExpr) *ioCloserFunc {
	fndecl, _ := typeutil.Callee(pp.pass.TypesInfo, call).(*types.Func)
	if fndecl == nil {
		return nil
	}

	fn := &ioCloserFunc{}
	if !pp.pass.ImportObjectFact(fndecl, fn) {
		return nil
	}

	return fn
}

func (pp *FunctionVisitor) getKnownCloser(call *ast.CallExpr) *ioCloserFunc {
	if fn := pp.findKnownReceiverFromCall(call); fn != nil {
		return fn
	}

	switch castedFun := call.Fun.(type) {
	case *ast.Ident:
		return pp.getKnownCloserFromIdent(castedFun)
	case *ast.SelectorExpr:
		return pp.getKnownCloserFromSelector(castedFun)
	}

	return nil
}

func (pp *FunctionVisitor) getKnownCloserFromIdent(id *ast.Ident) *ioCloserFunc {
	fndecl, ok := pp.pass.TypesInfo.ObjectOf(id).(*types.Func)
	if !ok {
		return nil
	}

	fn, ok := pp.receivers[fndecl]
	if !ok {
		return nil
	}

	return fn
}

func (pp *FunctionVisitor) getKnownCloserFromSelector(sel *ast.SelectorExpr) *ioCloserFunc {
	var knownCloser *ioCloserFunc

	pp.visitSelectors(sel, func(id *ast.Ident) bool {
		if fn := pp.getKnownCloserFromIdent(id); fn != nil {
			knownCloser = fn

			return false
		}

		return true
	})

	return knownCloser
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
	switch castedExpr := expr.(type) { // TODO: funclit
	case *ast.CallExpr:
		if castedExpr.Fun != nil && pp.closesIdentOnExpression(id, castedExpr.Fun) {
			return true
		}

		if cl := pp.getKnownCloser(castedExpr); cl != nil && cl.isCloser {
			return true
		}

	case *ast.SelectorExpr:
		if pp.isPosInExpression(id.Pos(), castedExpr.X) && castedExpr.Sel.Name == "Close" {
			return true
		}
	}

	return false
}

func (pp *FunctionVisitor) isPosInExpression(pos token.Pos, expr ast.Expr) bool {
	switch castedExpr := expr.(type) {
	case *ast.Ident:
		return pp.isIdentInPos(castedExpr, pos)
	case *ast.SelectorExpr:
		wasFound := false

		pp.visitSelectors(castedExpr, func(id *ast.Ident) bool {
			if pp.isIdentInPos(id, pos) {
				wasFound = true
				return false
			}

			return true
		})

		return wasFound
	}

	return false
}

func (pp *FunctionVisitor) visitSelectors(sel *ast.SelectorExpr, cb func(id *ast.Ident) bool) {
	if !cb(sel.Sel) {
		return
	}

	if newSel, ok := sel.X.(*ast.SelectorExpr); ok {
		pp.visitSelectors(newSel, cb)
	}
}

func (pp *FunctionVisitor) isIdentInPos(id *ast.Ident, pos token.Pos) bool {
	if pos == id.Pos() {
		return true
	}

	decl := pp.pass.TypesInfo.ObjectOf(id)

	return decl.Pos() == pos
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
