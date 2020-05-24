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

func (v *Visitor) isCloserType(closerType *types.Interface, t types.Type) bool {
	if types.Implements(t, closerType) {
		return true
	}

	// special case: a struct containing a Body field that implements io.Closer, like http.Response
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
		if v.Name() == "Body" && types.Implements(v.Type(), closerType) {
			return true
		}
	}

	return false
}

func (v *Visitor) returnsThatAreClosers(call *ast.CallExpr) []returnVar {
	switch t := v.pass.TypesInfo.Types[call].Type.(type) {
	case *types.Named:
		return []returnVar{
			{
				name:         t.String(),
				needsClosing: v.isCloserType(closerType, t),
			},
		}
	case *types.Pointer:
		return []returnVar{
			{
				name:         t.String(),
				needsClosing: v.isCloserType(closerType, t),
			},
		}
	case *types.Tuple:
		s := make([]returnVar, t.Len())

		for i := 0; i < t.Len(); i++ {
			switch et := t.At(i).Type().(type) {
			case *types.Named:
				s[i].name = et.String()
				s[i].needsClosing = v.isCloserType(closerType, et)
			case *types.Pointer:
				s[i].name = et.String()
				s[i].needsClosing = v.isCloserType(closerType, et)
			default:
				s[i].name = et.String()
				s[i].needsClosing = false
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
	switch n := rhs.(type) {
	case *ast.CallExpr:
		returnVars := v.returnsThatAreClosers(n)

		for i := 0; i < len(lhs); i++ {
			if id, ok := lhs[i].(*ast.Ident); ok {
				if id.Name == "_" && returnVars[i].needsClosing {
					v.pass.Reportf(id.Pos(), "%s should be closed", returnVars[i].name)
				}
			}
		}
	}
}

func (v *Visitor) handleMultiAssignment(lhs []ast.Expr, rhs []ast.Expr) {
	for i := 0; i < len(rhs); i++ {
		if id, ok := lhs[i].(*ast.Ident); ok {
			if call, ok := rhs[i].(*ast.CallExpr); ok {
				needsClosing, retName := v.callReturnsCloser(call)
				if id.Name == "_" && needsClosing {
					v.pass.Reportf(call.Pos(), "%s should be closed", retName)
				}
			}
		}
	}
}

// Do performs the visits to the code nodes
func (v *Visitor) Do(node ast.Node) bool {
	switch stmt := node.(type) {
	case *ast.ExprStmt:
		if call, ok := stmt.X.(*ast.CallExpr); ok {
			if ok, name := v.callReturnsCloser(call); ok {
				v.pass.Reportf(call.Pos(), "%s should be closed", name)
			}
		}
	case *ast.AssignStmt:
		if len(stmt.Rhs) == 1 {
			v.handleAssignment(stmt.Lhs, stmt.Rhs[0])
		} else {
			v.handleMultiAssignment(stmt.Lhs, stmt.Rhs)
		}
	case *ast.GoStmt:
		if ok, name := v.callReturnsCloser(stmt.Call); ok {
			v.pass.Reportf(stmt.Call.Pos(), "%s should be closed", name)
		}
	case *ast.DeferStmt:
		if ok, name := v.callReturnsCloser(stmt.Call); ok {
			v.pass.Reportf(stmt.Call.Pos(), "%s should be closed", name)
		}
	}

	return true
}
