package analyzer

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"log"
	"strings"
	"unicode"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"
)

var (
	enableAssignDebugger = false
	printFunctionFailure = false
)

// AssignVisitor is in charge of preprocessing packages to find functions that close io.Closers
type AssignVisitor struct {
	pass            *analysis.Pass
	closerFuncs     map[*types.Func]*ioCloserFunc
	localGlobalVars map[token.Pos]bool
}

func (av *AssignVisitor) debug(n ast.Node, text string, args ...interface{}) {
	if !enableAssignDebugger {
		return
	}

	if n == nil {
		return
	}

	pos := av.pass.Fset.Position(
		n.Pos(),
	)

	if strings.Contains(pos.Filename, "/libexec/") {
		log.Printf("Ignored debug: %v", pos.Filename)
		return
	}

	fmt.Printf(text+"\n", args...)

	_ = ast.Print(av.pass.Fset, n)
}

type posToClose struct {
	name                string
	typeName            string
	pos                 token.Pos
	parent              *ast.Ident
	wasClosedOrReturned bool
}

type field struct {
	name     string
	typeName string
	pos      token.Pos
}

type returnVar struct {
	needsClosing bool
	typeName     string
	fields       []field
}

func (av *AssignVisitor) newReturnVar(t types.Type) returnVar {
	if types.Implements(t, closerType) {
		return returnVar{
			needsClosing: true,
			typeName:     t.String(),
			fields:       []field{},
		}
	}

	// special case: a struct containing a io.Closer fields that implements io.Closer, like http.Response.Body
	ptr, ok := t.Underlying().(*types.Pointer)
	if !ok {
		return returnVar{
			needsClosing: false,
			fields:       []field{},
		}
	}

	str, ok := ptr.Elem().Underlying().(*types.Struct)
	if !ok {
		return returnVar{
			needsClosing: false,
			fields:       []field{},
		}
	}

	fields := []field{}

	for i := 0; i < str.NumFields(); i++ {
		v := str.Field(i)
		fieldName := v.Name()

		// TODO: don't ignore unexported fields if the struct is in the current package
		if types.Implements(v.Type(), closerType) && unicode.IsUpper([]rune(fieldName)[0]) {
			fields = append(fields, field{
				name:     fieldName,
				typeName: v.Type().String(),
				pos:      v.Pos(),
			})
		}
	}

	return returnVar{
		needsClosing: len(fields) > 0,
		typeName:     t.String(),
		fields:       fields,
	}
}

// this function checks functions that assign a closer
func (av *AssignVisitor) checkFunctionsThatAssignCloser() {
	for _, file := range av.pass.Files {
		for _, decl := range file.Decls {
			fdecl, ok := decl.(*ast.FuncDecl)
			if !ok || fdecl.Body == nil {
				continue
			}

			if !av.traverse(fdecl.Body.List) && printFunctionFailure {
				fmt.Println("Printing function that failed")

				_ = ast.Print(av.pass.Fset, fdecl)
			}
		}
	}
}

func (av *AssignVisitor) traverse(stmts []ast.Stmt) bool {
	posListToClose := []*posToClose{}

	for _, stmt := range stmts {
		if len(posListToClose) == 0 {
			switch castedStmt := stmt.(type) {
			case *ast.ExprStmt:
				call, ok := castedStmt.X.(*ast.CallExpr)
				if ok && av.callReturnsCloser(call) {
					av.pass.Reportf(call.Pos(), "return value won't be closed because it wasn't assigned") // FIXME: improve message
					return false
				}
			case *ast.DeferStmt:
				if av.callReturnsCloser(castedStmt.Call) {
					av.pass.Reportf(castedStmt.Call.Pos(), "return value won't be closed because it's on defer statement") // FIXME: improve message
					return false
				}
			case *ast.GoStmt:
				if av.callReturnsCloser(castedStmt.Call) {
					av.pass.Reportf(castedStmt.Call.Pos(), "return value won't be closed because it's on go statement") // FIXME: improve message
					return false
				}
			}
		}

		for _, idToClose := range posListToClose {
			if av.returnsOrClosesID(*idToClose, stmt) {
				idToClose.wasClosedOrReturned = true
			}
		}

		castedStmt, ok := stmt.(*ast.AssignStmt)
		if !ok {
			continue
		}

		if av.hasGlobalCloserInAssignment(castedStmt.Lhs) {
			continue
		}

		if len(castedStmt.Rhs) == 1 {
			posListToClose = append(posListToClose, av.handleAssignment(castedStmt.Lhs, castedStmt.Rhs[0])...)
		} else {
			posListToClose = append(posListToClose, av.handleMultiAssignment(castedStmt.Lhs, castedStmt.Rhs)...)
		}
	}

	for _, idToClose := range posListToClose {
		if !idToClose.wasClosedOrReturned {
			av.pass.Reportf(idToClose.parent.Pos(), "%s (%s) was not closed", idToClose.name, idToClose.typeName)
			return false
		}
	}

	return true
}

func (av *AssignVisitor) hasGlobalCloserInAssignment(lhs []ast.Expr) bool {
	for i := 0; i < len(lhs); i++ {
		assignedID, ok := lhs[i].(*ast.Ident)
		if !ok {
			continue
		}

		if av.shouldIgnoreGlobalVariable(assignedID) {
			return true
		}
	}

	return false
}

func (av *AssignVisitor) returnsOrClosesIDOnExpression(idToClose posToClose, expr ast.Expr) bool {
	switch cExpr := expr.(type) {
	case *ast.Ident:
		return av.getKnownCloserFromIdent(cExpr) != nil
	case *ast.FuncLit:
		return av.traverse(cExpr.Body.List)
	case *ast.CallExpr:
		return av.callsToKnownCloser(idToClose.pos, cExpr)
	case *ast.SelectorExpr:
		return av.getKnownCloserFromSelector(cExpr) != nil
	}

	return false
}

func (av *AssignVisitor) returnsOrClosesID(idToClose posToClose, stmt ast.Stmt) bool {
	switch castedStmt := stmt.(type) {
	case *ast.ReturnStmt:
		for _, res := range castedStmt.Results {
			if av.isPosInExpression(idToClose.pos, res) {
				return true
			}

			if idToClose.pos != idToClose.parent.Pos() && av.isPosInExpression(idToClose.parent.Pos(), res) {
				return true
			}
		}

	case *ast.DeferStmt:
		if av.callsToKnownCloser(idToClose.pos, castedStmt.Call) {
			return true
		}
	case *ast.GoStmt:
		if av.callsToKnownCloser(idToClose.pos, castedStmt.Call) {
			return true
		}
	case *ast.ExprStmt:
		call, ok := castedStmt.X.(*ast.CallExpr)
		if !ok {
			return false
		}

		if av.callReturnsCloser(call) {
			av.pass.Reportf(call.Pos(), "return value won't be closed because it wasn't assigned") // FIXME: improve message
			return false
		}

		for _, arg := range call.Args {
			if av.returnsOrClosesIDOnExpression(idToClose, arg) {
				return true
			}
		}

		if av.callsToKnownCloser(idToClose.pos, call) {
			return true
		}

	case *ast.AssignStmt:
		for _, exp := range castedStmt.Rhs {
			if call, ok := exp.(*ast.CallExpr); ok {
				if av.callsToKnownCloser(idToClose.pos, call) {
					return true
				}
			}
		}

	case *ast.IfStmt:
		if av.returnsOrClosesID(idToClose, castedStmt.Init) {
			return true
		}

		for _, stmt := range castedStmt.Body.List {
			if av.returnsOrClosesID(idToClose, stmt) {
				return true
			}
		}
	}

	return false
}

func (av *AssignVisitor) handleMultiAssignment(lhs []ast.Expr, rhs []ast.Expr) []*posToClose {
	posListToClose := make([]*posToClose, 0)

	for i := 0; i < len(rhs); i++ {
		id, ok := lhs[i].(*ast.Ident)
		if !ok {
			continue
		}

		call, ok := rhs[i].(*ast.CallExpr)
		if !ok {
			continue
		}

		returnVars := av.returnsThatAreClosers(call)

		if !returnVars[0].needsClosing {
			continue
		}

		if len(returnVars[0].fields) == 0 {
			posListToClose = append(posListToClose, &posToClose{
				parent:   id,
				name:     id.Name,
				typeName: returnVars[0].typeName,
				pos:      id.Pos(),
			})
		}

		for _, field := range returnVars[0].fields {
			posListToClose = append(posListToClose, &posToClose{
				parent:   id,
				name:     id.Name + "." + field.name,
				typeName: field.typeName,
				pos:      field.pos,
			})
		}
	}

	return posListToClose
}

func (av *AssignVisitor) handleAssignment(lhs []ast.Expr, rhs ast.Expr) []*posToClose {
	call, ok := rhs.(*ast.CallExpr)
	if !ok {
		return []*posToClose{}
	}

	returnVars := av.returnsThatAreClosers(call)
	posListToClose := make([]*posToClose, 0, len(returnVars))

	for i := 0; i < len(lhs); i++ {
		id, ok := lhs[i].(*ast.Ident)
		if !ok {
			continue
		}

		if !returnVars[i].needsClosing {
			continue
		}

		if len(returnVars[i].fields) == 0 {
			posListToClose = append(posListToClose, &posToClose{
				parent:   id,
				name:     id.Name,
				typeName: returnVars[i].typeName,
				pos:      id.Pos(),
			})
		}

		for _, field := range returnVars[i].fields {
			posListToClose = append(posListToClose, &posToClose{
				parent:   id,
				name:     id.Name + "." + field.name,
				typeName: field.typeName,
				pos:      field.pos,
			})
		}
	}

	// TODO: check that Rhs is not a call to a known av.closerFuncs

	return posListToClose
}

func (av *AssignVisitor) callReturnsCloser(call *ast.CallExpr) bool {
	for _, returnVar := range av.returnsThatAreClosers(call) {
		if returnVar.needsClosing {
			return true
		}
	}

	return false
}

func (av *AssignVisitor) returnsThatAreClosers(call *ast.CallExpr) []returnVar {
	if fn, ok := call.Fun.(*ast.SelectorExpr); ok && fn.Sel.Name == "NopCloser" {
		o, ok := fn.X.(*ast.Ident)
		if ok && (o.Name == "ioutil" || o.Name == "io") {
			return []returnVar{{}}
		}
	}

	switch t := av.pass.TypesInfo.Types[call].Type.(type) {
	case *types.Named:
		return []returnVar{av.newReturnVar(t)}
	case *types.Pointer:
		return []returnVar{av.newReturnVar(t)}
	case *types.Tuple:
		s := make([]returnVar, t.Len())

		for i := 0; i < t.Len(); i++ {
			switch et := t.At(i).Type().(type) {
			case *types.Named:
				s[i] = av.newReturnVar(et)
			case *types.Pointer:
				s[i] = av.newReturnVar(et)
			}
		}

		return s
	}

	return []returnVar{{}}
}

func (av *AssignVisitor) getKnownCloserFromIdent(id *ast.Ident) *ioCloserFunc {
	fndecl, ok := av.pass.TypesInfo.ObjectOf(id).(*types.Func)
	if !ok {
		return nil
	}

	fn, ok := av.closerFuncs[fndecl]
	if !ok {
		return nil
	}

	if av.pass.ImportObjectFact(fndecl, fn) && fn.isCloser {
		return fn
	}

	return fn
}

func (av *AssignVisitor) getKnownCloserFromSelector(sel *ast.SelectorExpr) *ioCloserFunc {
	var knownCloser *ioCloserFunc

	av.visitSelectors(sel, func(id *ast.Ident) bool {
		if id.Name == "Close" { // TODO: check that reciever is a io.Closer
			knownCloser = &ioCloserFunc{
				isCloser: true,
			} // this is a hack to mark "Close" as a known closer

			return false
		}

		if fn := av.getKnownCloserFromIdent(id); fn != nil {
			knownCloser = fn

			return false
		}

		return true
	})

	return knownCloser
}

func (av *AssignVisitor) visitSelectors(sel *ast.SelectorExpr, cb func(id *ast.Ident) bool) {
	if !cb(sel.Sel) {
		return
	}

	if newSel, ok := sel.X.(*ast.SelectorExpr); ok {
		av.visitSelectors(newSel, cb)
	}
}

func (av *AssignVisitor) findKnownReceiverFromCall(pos token.Pos, call *ast.CallExpr) *ioCloserFunc {
	fndecl, _ := typeutil.Callee(av.pass.TypesInfo, call).(*types.Func)
	if fndecl == nil {
		return nil
	}

	fn := &ioCloserFunc{}
	av.pass.ImportObjectFact(fndecl, fn)

	return fn
}

func (av *AssignVisitor) callsToKnownCloser(pos token.Pos, call *ast.CallExpr) bool {
	fndecl, _ := typeutil.Callee(av.pass.TypesInfo, call).(*types.Func)
	fn := &ioCloserFunc{}

	if fndecl != nil && av.pass.ImportObjectFact(fndecl, fn) && fn != nil {
		return fn.isCloser
	}

	switch castedFun := call.Fun.(type) {
	case *ast.CallExpr:
		return av.callsToKnownCloser(pos, castedFun)
	case *ast.Ident:
		return av.getKnownCloserFromIdent(castedFun) != nil
	case *ast.SelectorExpr:
		return av.getKnownCloserFromSelector(castedFun) != nil
	case *ast.FuncLit:
		return av.traverse(castedFun.Body.List)
	}

	// TODO: check that call.Args match with the params that are received and closed by "fn"

	return fn.isCloser
}

func (av *AssignVisitor) isPosInExpression(pos token.Pos, expr ast.Expr) bool {
	switch castedExpr := expr.(type) {
	case *ast.UnaryExpr:
		return av.isPosInExpression(pos, castedExpr.X)
	case *ast.CallExpr:
		return av.findKnownReceiverFromCall(pos, castedExpr) != nil
	case *ast.Ident:
		return av.isIdentInPos(castedExpr, pos)
	case *ast.SelectorExpr:
		wasFound := false

		av.visitSelectors(castedExpr, func(id *ast.Ident) bool {
			if av.isIdentInPos(id, pos) {
				wasFound = true
				return false
			}

			return true
		})

		return wasFound
	case *ast.CompositeLit:
		if castedExpr.Elts == nil {
			break
		}

		for _, e := range castedExpr.Elts {
			if av.isPosInExpression(pos, e) {
				return true
			}
		}
	case *ast.KeyValueExpr:
		// FIXME: this is not 100% accurate because we haven't checked that the Close is called in the future
		if av.isPosInExpression(pos, castedExpr.Value) {
			return true
		}
	}

	return false
}

func (av *AssignVisitor) isIdentInPos(id *ast.Ident, pos token.Pos) bool {
	if pos == id.Pos() {
		return true
	}

	decl := av.pass.TypesInfo.ObjectOf(id)

	return decl.Pos() == pos
}

func (av *AssignVisitor) isExprEqualToIdent(id *ast.Ident, x ast.Expr) bool {
	xIdent, ok := x.(*ast.Ident)
	if !ok {
		return false
	}

	if id.NamePos == xIdent.NamePos {
		return true
	}

	decl := av.pass.TypesInfo.ObjectOf(xIdent)

	return decl.Pos() == id.Pos()
}

func (av *AssignVisitor) shouldIgnoreGlobalVariable(id *ast.Ident) bool {
	if id.Obj == nil || id.Obj.Decl == nil {
		return false
	}

	val, ok := id.Obj.Decl.(*ast.ValueSpec)
	if !ok {
		return false
	}

	if len(val.Names) == 1 && av.localGlobalVars[val.Names[0].NamePos] {
		return true
	}

	return false
}
