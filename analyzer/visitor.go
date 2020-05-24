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

func (v *Visitor) handleAssignment(lhs []ast.Expr, rhs ast.Expr) {
	call, ok := rhs.(*ast.CallExpr)
	if !ok {
		return
	}

	returnVars := v.returnsThatAreClosers(call)

	for i := 0; i < len(lhs); i++ {
		if id, ok := lhs[i].(*ast.Ident); ok {
			if id.Name == "_" && returnVars[i].needsClosing {
				v.pass.Reportf(id.Pos(), "%s should be closed", returnVars[i].name)
			}
		}
	}
}

func (v *Visitor) handleMultiAssignment(lhs []ast.Expr, rhs []ast.Expr) {
	for i := 0; i < len(rhs); i++ {
		id, ok := lhs[i].(*ast.Ident)
		if !ok {
			continue
		}

		call, ok := rhs[i].(*ast.CallExpr)
		if !ok {
			continue
		}

		needsClosing, retName := v.callReturnsCloser(call)
		if id.Name == "_" && needsClosing {
			v.pass.Reportf(call.Pos(), "%s should be closed", retName)
		}
	}
}

func (v *Visitor) handleCall(call *ast.CallExpr) {
	if ok, name := v.callReturnsCloser(call); ok {
		v.report(call, name)
	}
}

// Do performs the visits to the code nodes
func (v *Visitor) Do(node ast.Node) bool {
	switch stmt := node.(type) {
	case *ast.ExprStmt:
		if call, ok := stmt.X.(*ast.CallExpr); ok {
			v.handleCall(call)
		}
	case *ast.AssignStmt:
		if len(stmt.Rhs) == 1 {
			v.handleAssignment(stmt.Lhs, stmt.Rhs[0])
		} else {
			v.handleMultiAssignment(stmt.Lhs, stmt.Rhs)
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
