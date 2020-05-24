package analyzer

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/packages"
)

var (
	// Analyzer defines the analyzer for check-close
	Analyzer = &analysis.Analyzer{
		Name:     "checkclose",
		Doc:      "check for any closable return value",
		Run:      run,
		Requires: []*analysis.Analyzer{inspect.Analyzer},
		//FactTypes: []analysis.Fact{new(closer)},
	}

	closerType *types.Interface
)

func run(pass *analysis.Pass) (interface{}, error) {
	println("running analysis...", pass.Pkg.Name())

	findFunctionsThatReturnCloser(pass)

	return nil, nil
}

func findFunctionsThatReturnCloser(pass *analysis.Pass) {
	visitor := &Visitor{
		pass: pass,
	}

	for _, f := range pass.Files {
		ast.Inspect(f, visitor.Do)
	}
}

func isCloserType(closerType *types.Interface, t types.Type) bool {
	println(t.String(), "implements", closerType.String(), "?", types.Implements(t, closerType))

	ptr, ok := t.Underlying().(*types.Pointer)
	if ok {
		println("is a pointer!", ptr.String())

		str := ptr.Elem().Underlying().(*types.Struct)

		if ok {
			println("is a struct!", str.String())

			for i := 0; i < str.NumFields(); i++ {
				v := str.Field(i)
				if v.Name() == "Body" && types.Implements(v.Type(), closerType) {
					println("body found!")

					return true
				}
			}
		}
	}

	return types.Implements(t, closerType)
}

// init finds the io.Closer interface
func init() {
	cfg := &packages.Config{Mode: packages.NeedTypes, Tests: false}

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
