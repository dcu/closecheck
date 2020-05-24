package analyzer

import (
	"fmt"
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

// Visitor visits the code nodes looking for variables that need to be closed
type Visitor struct {
	pass *analysis.Pass
}

func (v *Visitor) callReturnsCloser(call *ast.CallExpr) bool {
	for _, isCloser := range v.returnsThatAreClosers(call) {
		if isCloser {
			return true
		}
	}

	return false
}

func (v *Visitor) returnsThatAreClosers(call *ast.CallExpr) []bool {
	switch t := v.pass.TypesInfo.Types[call].Type.(type) {
	case *types.Named:
		println(t.String(), isCloserType(closerType, t))
		return []bool{isCloserType(closerType, t)}
	case *types.Pointer:
		println(t.String(), isCloserType(closerType, t))
		return []bool{isCloserType(closerType, t)}
	case *types.Tuple:
		s := make([]bool, t.Len())

		for i := 0; i < t.Len(); i++ {
			switch et := t.At(i).Type().(type) {
			case *types.Named:
				s[i] = isCloserType(closerType, et)
			case *types.Pointer:
				s[i] = isCloserType(closerType, et)
			default:
				s[i] = false
			}
		}

		fmt.Printf("tuple: %#v\n", s)

		return s
	default:
		println(t.String(), "was not handled")
	}

	return []bool{false}
}

func (v *Visitor) handleAssignment(lhs []ast.Expr, rhs ast.Expr) {
	switch n := rhs.(type) {
	case *ast.CallExpr:
		isCloserAtPos := v.returnsThatAreClosers(n)

		for i := 0; i < len(lhs); i++ {
			if id, ok := lhs[i].(*ast.Ident); ok {
				if id.Name == "_" && isCloserAtPos[i] {
					fmt.Println("this should be marked as error because the closer is asigned to _")
				}
			}
		}
	}
}

func (v *Visitor) handleMultiAssignment(lhs []ast.Expr, rhs []ast.Expr) {
	for i := 0; i < len(rhs); i++ {
		if id, ok := lhs[i].(*ast.Ident); ok {
			if call, ok := rhs[i].(*ast.CallExpr); ok {
				if id.Name == "_" && v.callReturnsCloser(call) {
					fmt.Println("this should be marked as error because the closer is asigned to _ in multi assign expression")
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
			if v.callReturnsCloser(call) {
				fmt.Println("this should fail because it's a statement with not handled closer")
			}
		}
	case *ast.AssignStmt:
		if len(stmt.Rhs) == 1 {
			v.handleAssignment(stmt.Lhs, stmt.Rhs[0])
		} else {
			v.handleMultiAssignment(stmt.Lhs, stmt.Rhs)
		}
	}

	return true
}
