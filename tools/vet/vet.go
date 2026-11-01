// Package vet holds Atlas's own vet checks: the rules the spec states as "a
// vet check" (ATLAS-PACKAGES-008, ATLAS-MECH-012) and the mechanism rules of
// the pattern library that a compiler can see. Test files and generated
// files are not checked.
package vet

import (
	"go/ast"
	"go/types"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// Analyzers is every check, in the order atlasvet runs them.
var Analyzers = []*analysis.Analyzer{NoInit, NoBareGo, NoGetenv, NoHTTPError}

// NoInit refuses init functions: what a deployable is built from is registered
// in its one list, never from an init function (ATLAS-PACKAGES-008, §6.5.8).
// A function documented with "atlas:allow-init <reason>" is let through.
var NoInit = &analysis.Analyzer{
	Name: "noinit",
	Doc:  "refuse init functions; register modules in the deployable's one list (ATLAS-PACKAGES-008)",
	Run: func(pass *analysis.Pass) (any, error) {
		for _, f := range checked(pass) {
			for _, d := range f.Decls {
				fd, ok := d.(*ast.FuncDecl)
				if !ok || fd.Recv != nil || fd.Name.Name != "init" || allowed(fd.Doc, "atlas:allow-init") {
					continue
				}
				pass.Reportf(fd.Pos(), "init function: register in the deployable's one list instead (ATLAS-PACKAGES-008)")
			}
		}
		return nil, nil
	},
}

// NoBareGo refuses a go statement outside sdk/go/kit: every goroutine starts
// through kit/conc's Go, which recovers a panic and reports it
// (ATLAS-MECH-012).
var NoBareGo = &analysis.Analyzer{
	Name: "nobarego",
	Doc:  "refuse bare go statements outside sdk/go/kit; use kit/conc.Go (ATLAS-MECH-012)",
	Run: func(pass *analysis.Pass) (any, error) {
		if inKit(pass.Pkg.Path()) {
			return nil, nil
		}
		for _, f := range checked(pass) {
			ast.Inspect(f, func(n ast.Node) bool {
				if g, ok := n.(*ast.GoStmt); ok {
					pass.Reportf(g.Pos(), "bare go statement: start it with kit/conc.Go so a panic is recovered and reported (ATLAS-MECH-012)")
				}
				return true
			})
		}
		return nil, nil
	},
}

// NoGetenv refuses reading the environment outside a composition root
// (a package under cmd/), a package named config or settings, or a file
// named settings.go: settings are parsed once and passed down.
var NoGetenv = &analysis.Analyzer{
	Name: "nogetenv",
	Doc:  "refuse os.Getenv and friends outside cmd/, config and settings",
	Run: func(pass *analysis.Pass) (any, error) {
		p := pass.Pkg
		if strings.Contains(p.Path()+"/", "/cmd/") || p.Name() == "config" || p.Name() == "settings" {
			return nil, nil
		}
		for _, f := range checked(pass) {
			if filepath.Base(pass.Fset.File(f.Pos()).Name()) == "settings.go" {
				continue
			}
			ast.Inspect(f, func(n ast.Node) bool {
				if fn := called(pass, n); fn != nil && fn.Pkg() != nil && fn.Pkg().Path() == "os" && envFuncs[fn.Name()] {
					pass.Reportf(n.Pos(), "os.%s outside a composition root or settings: read settings once and pass them down", fn.Name())
				}
				return true
			})
		}
		return nil, nil
	},
}

// NoHTTPError refuses http.Error, which writes text/plain: every failure is a
// problem document written by kit/problem (§6.7.3, one error envelope).
var NoHTTPError = &analysis.Analyzer{
	Name: "nohttperror",
	Doc:  "refuse http.Error; write a problem with kit/problem",
	Run: func(pass *analysis.Pass) (any, error) {
		for _, f := range checked(pass) {
			ast.Inspect(f, func(n ast.Node) bool {
				if fn := called(pass, n); fn != nil && fn.Pkg() != nil && fn.Pkg().Path() == "net/http" && fn.Name() == "Error" {
					pass.Reportf(n.Pos(), "http.Error writes text/plain: write a problem with kit/problem")
				}
				return true
			})
		}
		return nil, nil
	},
}

var envFuncs = map[string]bool{"Getenv": true, "LookupEnv": true, "Environ": true, "ExpandEnv": true}

// checked returns the files a check applies to: not tests, not generated.
func checked(pass *analysis.Pass) []*ast.File {
	var out []*ast.File
	for _, f := range pass.Files {
		name := pass.Fset.File(f.Pos()).Name()
		if strings.HasSuffix(name, "_test.go") || ast.IsGenerated(f) {
			continue
		}
		out = append(out, f)
	}
	return out
}

// called is the function a call expression calls, when it is a package-level
// function or method the type checker resolved.
func called(pass *analysis.Pass, n ast.Node) *types.Func {
	call, ok := n.(*ast.CallExpr)
	if !ok {
		return nil
	}
	var id *ast.Ident
	switch fun := ast.Unparen(call.Fun).(type) {
	case *ast.Ident:
		id = fun
	case *ast.SelectorExpr:
		id = fun.Sel
	default:
		return nil
	}
	fn, _ := pass.TypesInfo.Uses[id].(*types.Func)
	return fn
}

func inKit(pkg string) bool {
	return strings.Contains(pkg+"/", "/sdk/go/kit/")
}

func allowed(doc *ast.CommentGroup, marker string) bool {
	return doc != nil && strings.Contains(doc.Text(), marker)
}
