package analyzer

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
	"golang.org/x/tools/go/packages"
)

var (
	// Analyzer defines the analyzer for closecheck
	Analyzer = &analysis.Analyzer{
		Name:     "closecheck",
		Doc:      "check that any io.Closer in return a value is closed",
		Run:      run,
		Requires: []*analysis.Analyzer{inspect.Analyzer},
	}

	closerType          *types.Interface
	printStatementsMode bool
)

func run(pass *analysis.Pass) (interface{}, error) {
	visitor := &Visitor{
		pass:       pass,
		globalVars: map[token.Pos]bool{},
	}

	stack := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)

	stack.WithStack([]ast.Node{&ast.ExprStmt{}, &ast.AssignStmt{}, &ast.GoStmt{}, &ast.DeferStmt{}, &ast.Ident{}, &ast.CallExpr{}, &ast.DeclStmt{}}, visitor.Do)

	return nil, nil
}

func init() {
	Analyzer.Flags.BoolVar(&printStatementsMode, "print-statements", false, "print program trace")
}

// init finds the io.Closer interface
func init() {
	cfg := &packages.Config{Mode: packages.NeedDeps | packages.NeedTypes, Tests: false}

	pkgs, err := packages.Load(cfg, "io")
	if err != nil {
		panic(err)
	}

	if len(pkgs) != 1 {
		panic("couldn't load io package")
	}

	closerType = pkgs[0].Types.Scope().Lookup("Closer").Type().Underlying().(*types.Interface)
	if closerType == nil {
		panic("io.Closer not found")
	}
}
