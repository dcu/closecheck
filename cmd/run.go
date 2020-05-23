package cmd

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"path/filepath"

	"github.com/spf13/cobra"
	"golang.org/x/tools/go/packages"
)

// runCmd represents the run command
var runCmd = &cobra.Command{
	Use:   "run",
	Short: "A brief description of your command",
	Long:  ``,
	RunE: func(cmd *cobra.Command, args []string) error {
		srcPath := args[0]
		fset := token.NewFileSet()

		fmt.Printf("Parsing source file %s...\n", srcPath)

		f, err := parser.ParseFile(fset, srcPath, nil, 0)
		if err != nil {
			return err
		}

		fmt.Println("Found imports:")

		//pkgsToLoad := make([]string, 0, len(f.Imports))
		for _, s := range f.Imports {
			fmt.Println(s.Path.Value)

			//pkgName, _ := strconv.Unquote(s.Path.Value)
			//pkgsToLoad = append(pkgsToLoad, pkgName)
		}

		absPath, _ := filepath.Abs(filepath.Dir(srcPath))

		cfg := &packages.Config{Mode: packages.NeedName | packages.NeedImports | packages.NeedDeps | packages.NeedTypes | packages.NeedSyntax | packages.NeedTypesInfo, Tests: false}
		pkgs, err := packages.Load(cfg, absPath)
		if err != nil {
			return err
		}

		packages.PrintErrors(pkgs)

		var ioPkg *packages.Package

		packages.Visit(pkgs, nil, func(pkg *packages.Package) {
			if pkg.Name == "io" {
				ioPkg = pkg
			}
		})

		if ioPkg == nil {
			return errors.New("io package not imported")
		}

		closerType := ioPkg.Types.Scope().Lookup("Closer").Type().Underlying().(*types.Interface)

		println("packages:", len(pkgs))
		println(closerType.String())

		for _, pkg := range pkgs {
			for _, f := range pkg.Syntax {
				ast.Inspect(f, func(n ast.Node) bool {
					//_ = ast.Print(fset, n)

					switch x := n.(type) {
					case *ast.CallExpr:
						//_ = ast.Print(fset, x.Fun)

						hasCloser(pkg, closerType, x)

						selexpr, ok := x.Fun.(*ast.SelectorExpr)
						if !ok {
							return true
						}

						//ident, ok := selexpr.X.(*ast.Ident)
						//if !ok {
						//return true
						//}

						//pos := fset.Position(selexpr.Sel.Pos())

						fn, ok := pkg.TypesInfo.ObjectOf(selexpr.Sel).(*types.Func)

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
				})
			}
		}

		return nil
	},
}

func hasCloser(pkg *packages.Package, closerType *types.Interface, call *ast.CallExpr) []bool {
	switch t := pkg.TypesInfo.Types[call].Type.(type) {
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

func init() {
	rootCmd.AddCommand(runCmd)
}
