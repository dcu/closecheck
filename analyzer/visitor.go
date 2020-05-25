package analyzer

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

// Visitor visits the code nodes looking for variables that need to be closed
type Visitor struct {
	pass *analysis.Pass

	globalVars map[token.Pos]bool
}

type returnVar struct {
	name         string
	needsClosing bool
}

func (v *Visitor) callReturnsCloser(call *ast.CallExpr) (bool, string) {
	for _, returnVar := range v.returnsThatAreClosers(call) {
		if returnVar.needsClosing {
			return true, returnVar.name
		}
	}

	return false, ""
}

func (v *Visitor) newReturnVar(t types.Type) returnVar {
	if types.Implements(t, closerType) {
		return returnVar{
			name:         t.String(),
			needsClosing: true,
		}
	}

	// special case: a struct containing a io.Closer fields that implements io.Closer, like http.Response.Body
	ptr, ok := t.Underlying().(*types.Pointer)
	if !ok {
		return returnVar{
			name:         t.String(),
			needsClosing: false,
		}
	}

	str, ok := ptr.Elem().Underlying().(*types.Struct)
	if !ok {
		return returnVar{
			name:         t.String(),
			needsClosing: false,
		}
	}

	for i := 0; i < str.NumFields(); i++ {
		v := str.Field(i)
		if types.Implements(v.Type(), closerType) {
			return returnVar{
				name:         t.String() + "." + v.Name(),
				needsClosing: true,
			}
		}
	}

	return returnVar{
		name:         t.String(),
		needsClosing: false,
	}
}
func (v *Visitor) returnsThatAreClosers(call *ast.CallExpr) []returnVar {
	if fn, ok := call.Fun.(*ast.SelectorExpr); ok && fn.Sel.Name == "NopCloser" {
		o, ok := fn.X.(*ast.Ident)
		if ok && o.Name == "ioutil" {
			return []returnVar{{}}
		}
	}

	switch t := v.pass.TypesInfo.Types[call].Type.(type) {
	case *types.Named:
		return []returnVar{v.newReturnVar(t)}
	case *types.Pointer:
		return []returnVar{v.newReturnVar(t)}
	case *types.Tuple:
		s := make([]returnVar, t.Len())

		for i := 0; i < t.Len(); i++ {
			switch et := t.At(i).Type().(type) {
			case *types.Named:
				s[i] = v.newReturnVar(et)
			case *types.Pointer:
				s[i] = v.newReturnVar(et)
			}
		}

		return s
	}

	return []returnVar{
		{
			name:         "",
			needsClosing: false,
		},
	}
}

func (v *Visitor) identIsCloser(ident *ast.Ident) bool {
	switch t := v.pass.TypesInfo.Types[ident].Type.(type) {
	case *types.Named:
		return types.Implements(t, closerType)
	case *types.Pointer:
		return types.Implements(t, closerType)
	default:
		println("cannot determine if ident is a closer", ident.Name, t)
	}

	return false
}

func (v *Visitor) handleAssignment(stack []ast.Node, lhs []ast.Expr, rhs ast.Expr) {
	call, ok := rhs.(*ast.CallExpr)
	if !ok {
		return
	}

	returnVars := v.returnsThatAreClosers(call)

	for i := 0; i < len(lhs); i++ {
		id, ok := lhs[i].(*ast.Ident)
		if !ok {
			continue
		}

		if v.shouldIgnoreGlobalVariable(id) {
			continue
		}

		if returnVars[i].needsClosing {
			if id.Name == "_" {
				v.pass.Reportf(id.Pos(), "%s should be closed", returnVars[i].name)
			} else if !v.ensureCloseOrReturnOnID(stack, id) {
				v.pass.Reportf(id.Pos(), "%s should be closed", returnVars[i].name)
			}
		}
	}
}

func (v *Visitor) shouldIgnoreGlobalVariable(id *ast.Ident) bool {
	if id.Obj == nil || id.Obj.Decl == nil {
		return false
	}

	val, ok := id.Obj.Decl.(*ast.ValueSpec)
	if !ok {
		return false
	}

	if len(val.Names) == 1 && v.globalVars[val.Names[0].NamePos] {
		return true
	}

	return false
}

func (v *Visitor) ensureCloseOrReturnOnID(stack []ast.Node, id *ast.Ident) bool {
	stmts := v.statementsForCurrentBlock(stack)

	return v.ensureCloseOrReturnOnStatements(id, stmts)
}

func (v *Visitor) ensureCloseOrReturnOnStatements(id *ast.Ident, stmts []ast.Stmt) bool {
	for _, stmt := range stmts {
		switch castedStmt := stmt.(type) {
		case *ast.ReturnStmt:
			if v.ensureCloseOrReturnOnExpressions(id, castedStmt.Results) {
				return true
			}
		case *ast.DeferStmt:
			if v.ensureCloseOrReturnOnExpressions(id, []ast.Expr{castedStmt.Call}) {
				return true
			}
		case *ast.ExprStmt:
			if v.ensureCloseOrReturnOnExpressions(id, []ast.Expr{castedStmt.X}) {
				return true
			}
		case *ast.AssignStmt:
			if v.ensureCloseOrReturnOnExpressions(id, castedStmt.Rhs) {
				return true
			}
		case *ast.BlockStmt:
			return v.ensureCloseOrReturnOnStatements(id, castedStmt.List)
		}
	}

	return false
}

func compareIdents(id1 *ast.Ident, id2 *ast.Ident) bool {
	return id1.Obj == id2.Obj
}

func (v *Visitor) ensureCloseOrReturnOnExpressions(id *ast.Ident, exprs []ast.Expr) bool {
	for _, expr := range exprs {
		switch castedExpr := expr.(type) {
		case *ast.Ident:
			if compareIdents(castedExpr, id) {
				return true
			} else if castedExpr.Obj != nil && castedExpr.Obj.Kind == ast.Fun {
				funcDecl, ok := castedExpr.Obj.Decl.(*ast.FuncDecl)
				if !ok {
					continue
				}

				if v.ensureCloseOrReturnOnStatements(id, []ast.Stmt{funcDecl.Body}) {
					return true
				}
			}
		case *ast.CallExpr:
			for _, arg := range castedExpr.Args {
				wasIdentFound := false

				// the closable was passed to a different function
				// TODO: check that the function closes the received object.
				// TODO: check that the passed object is actually a io.Closer

				v.visitIdents(arg, func(idSel *ast.Ident) {
					if compareIdents(idSel, id) {
						wasIdentFound = true
					}
				})

				if wasIdentFound {
					return true
				}
			}

			if v.ensureCloseOrReturnOnExpressions(id, []ast.Expr{castedExpr.Fun}) {
				return true
			}
		case *ast.SelectorExpr:
			if v.ensureCloseOrReturnSelector(id, castedExpr) {
				return true
			}
		case *ast.FuncLit:
			if v.ensureCloseOrReturnOnStatements(id, castedExpr.Body.List) {
				return true
			}
		}
	}

	return false
}

func (v *Visitor) ensureCloseOrReturnSelector(id *ast.Ident, sel *ast.SelectorExpr) bool {
	wasClosed := false

	v.visitIdents(sel, func(visitedID *ast.Ident) {
		if visitedID.Name == "Close" { // FIXME: this is not accurate
			wasClosed = true

			return
		}
	})

	return wasClosed
}

func (v *Visitor) visitIdents(n ast.Node, cb func(id *ast.Ident)) {
	switch n := n.(type) {
	case *ast.SelectorExpr:
		cb(n.Sel)

		v.visitIdents(n.X, cb)
	case *ast.Ident:
		cb(n)
	}
}

func (v *Visitor) statementsForCurrentBlock(stack []ast.Node) []ast.Stmt {
	for i := len(stack) - 1; i >= 0; i-- {
		block, ok := stack[i].(*ast.BlockStmt)
		if !ok {
			continue
		}

		for j, v := range block.List {
			if v == stack[i+1] {
				return block.List[j:]
			}
		}

		break
	}

	return nil
}

func (v *Visitor) handleMultiAssignment(stack []ast.Node, lhs []ast.Expr, rhs []ast.Expr) {
	for i := 0; i < len(rhs); i++ {
		id, ok := lhs[i].(*ast.Ident)
		if !ok {
			continue
		}

		call, ok := rhs[i].(*ast.CallExpr)
		if !ok {
			continue
		}

		if id.Name == "_" {
			v.handleCall(call)
		}
	}
}

func (v *Visitor) handleCall(call *ast.CallExpr) {
	if ok, name := v.callReturnsCloser(call); ok {
		v.report(call, name)
	}
}

// Do performs the visits to the code nodes
func (v *Visitor) Do(node ast.Node, push bool, stack []ast.Node) bool {
	if printStatementsMode {
		fmt.Println("------ statement --------", push)

		_ = ast.Print(v.pass.Fset, node)
	}

	if !push {
		return true
	}

	if id, ok := node.(*ast.Ident); ok && id.Obj != nil && id.Obj.Decl != nil && id.Obj.Kind == ast.Var {
		v.globalVars[id.NamePos] = true
	}

	switch stmt := node.(type) {
	case *ast.ExprStmt:
		if call, ok := stmt.X.(*ast.CallExpr); ok {
			v.handleCall(call)
		}
	case *ast.AssignStmt:
		if len(stmt.Rhs) == 1 {
			v.handleAssignment(stack, stmt.Lhs, stmt.Rhs[0])
		} else {
			v.handleMultiAssignment(stack, stmt.Lhs, stmt.Rhs)
		}
	case *ast.GoStmt:
		v.handleCall(stmt.Call)
	case *ast.DeferStmt:
		v.handleCall(stmt.Call)
	}

	return true
}

func (v *Visitor) report(node ast.Node, name string) {
	v.pass.Reportf(node.Pos(), "%s should be closed", name)
}
