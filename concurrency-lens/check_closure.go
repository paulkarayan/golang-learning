package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/token"
	"os"
	"time"
)

// ClosureFinding is a goroutine that captures a loop variable unsafely.
type ClosureFinding struct {
	File          string `json:"file"`
	Line          int    `json:"line"`
	GoroutineLine int    `json:"goroutine_line"`
	VarName       string `json:"var_name"`
	VarKind       string `json:"var_kind"`  // loop_var | outer_var
	LoopKind      string `json:"loop_kind"` // for_range | for_classic | none
	FuncContext   string `json:"func_context"`
	Snippet       string `json:"snippet"`
	Severity      string `json:"severity"`
	Issue         string `json:"issue"`
	GoVersionSafe bool   `json:"go_version_safe,omitempty"`
}

func runCheckClosure(args []string) error {
	fs := flag.NewFlagSet("check-closure", flag.ExitOnError)
	dir := fs.String("dir", "", "project directory to analyse")
	output := fs.String("o", "", "output JSON file (default: auto-named in current dir)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dir == "" {
		return fmt.Errorf("-dir is required")
	}

	start := time.Now()
	findings, err := checkClosure(*dir)
	if err != nil {
		return err
	}

	items := make([]json.RawMessage, 0, len(findings))
	for _, f := range findings {
		items = append(items, MarshalItem(f))
	}

	result := CheckResult{
		Check:      "check-closure",
		ProjectDir: absPath(*dir),
		RunAt:      start,
		DurationMs: time.Since(start).Milliseconds(),
		ItemCount:  len(items),
		Items:      items,
	}

	outPath := *output
	if outPath == "" {
		outPath = OutputFileName(*dir, "check-closure", start)
	}
	if err := WriteCheckResult(result, outPath); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "check-closure: %d finding(s) → %s\n", len(findings), outPath)
	return nil
}

// ---- implementation ----

type loopInfo struct {
	kind string // for_range | for_classic
	vars []string
	node ast.Node
}

type closureVisitor struct {
	fset      *token.FileSet
	src       []byte
	path      string
	goVersion string
	findings  []ClosureFinding

	nodeStack []ast.Node
	loopStack []loopInfo
}

func (v *closureVisitor) Visit(n ast.Node) ast.Visitor {
	if n == nil {
		if len(v.nodeStack) > 0 {
			leaving := v.nodeStack[len(v.nodeStack)-1]
			v.nodeStack = v.nodeStack[:len(v.nodeStack)-1]
			switch leaving.(type) {
			case *ast.ForStmt, *ast.RangeStmt:
				if len(v.loopStack) > 0 {
					v.loopStack = v.loopStack[:len(v.loopStack)-1]
				}
			}
		}
		return nil
	}

	v.nodeStack = append(v.nodeStack, n)

	switch stmt := n.(type) {
	case *ast.RangeStmt:
		info := loopInfo{kind: "for_range", node: stmt}
		if ident, ok := stmt.Key.(*ast.Ident); ok && ident.Name != "_" {
			info.vars = append(info.vars, ident.Name)
		}
		if stmt.Value != nil {
			if ident, ok := stmt.Value.(*ast.Ident); ok && ident.Name != "_" {
				info.vars = append(info.vars, ident.Name)
			}
		}
		v.loopStack = append(v.loopStack, info)

	case *ast.ForStmt:
		info := loopInfo{kind: "for_classic", node: stmt}
		if stmt.Init != nil {
			if assign, ok := stmt.Init.(*ast.AssignStmt); ok {
				for _, lhs := range assign.Lhs {
					if ident, ok := lhs.(*ast.Ident); ok {
						info.vars = append(info.vars, ident.Name)
					}
				}
			}
		}
		v.loopStack = append(v.loopStack, info)

	case *ast.GoStmt:
		if len(v.loopStack) > 0 {
			v.checkGoInLoop(stmt)
		}
	}

	return v
}

func (v *closureVisitor) checkGoInLoop(goStmt *ast.GoStmt) {
	top := v.loopStack[len(v.loopStack)-1]
	if len(top.vars) == 0 {
		return
	}

	// Only care about literal closures — `go func() { ... }()`
	funcLit, ok := goStmt.Call.Fun.(*ast.FuncLit)
	if !ok {
		return
	}

	// Collect parameter names — these are safe (passed by value as args)
	paramNames := make(map[string]bool)
	if funcLit.Type.Params != nil {
		for _, field := range funcLit.Type.Params.List {
			for _, name := range field.Names {
				paramNames[name.Name] = true
			}
		}
	}

	// For each loop variable, check if the body references it without it being a param
	seen := make(map[string]bool)
	ast.Inspect(funcLit.Body, func(inner ast.Node) bool {
		ident, ok := inner.(*ast.Ident)
		if !ok {
			return true
		}
		for _, lv := range top.vars {
			if ident.Name == lv && !paramNames[lv] && !seen[lv] {
				seen[lv] = true
				pos := v.fset.Position(goStmt.Pos())
				identPos := v.fset.Position(ident.Pos())

				severity := "BUG"
				issue := fmt.Sprintf("goroutine closes over loop variable %q without passing it as an argument", lv)
				goVersionSafe := false

				// Go 1.22+ fixed per-iteration variable semantics for range loops
				if top.kind == "for_range" && goVersionAtLeast(v.goVersion, 1, 22) {
					goVersionSafe = true
					severity = "INFO"
					issue += " (safe in Go 1.22+ — loop variable is now per-iteration)"
				}

				snippet := snippetLines(v.src, v.fset, top.node.Pos(), 3)

				// find enclosing function name from node stack
				funcCtx := "<unknown>"
				for i := len(v.nodeStack) - 1; i >= 0; i-- {
					if fn, ok := v.nodeStack[i].(*ast.FuncDecl); ok {
						funcCtx = funcName(fn)
						break
					}
				}

				v.findings = append(v.findings, ClosureFinding{
					File:          v.path,
					Line:          identPos.Line,
					GoroutineLine: pos.Line,
					VarName:       lv,
					VarKind:       "loop_var",
					LoopKind:      top.kind,
					FuncContext:   funcCtx,
					Snippet:       snippet,
					Severity:      severity,
					Issue:         issue,
					GoVersionSafe: goVersionSafe,
				})
			}
		}
		return true
	})
}

func checkClosure(projectDir string) ([]ClosureFinding, error) {
	goVersion := readGoVersion(projectDir)
	var allFindings []ClosureFinding

	err := walkProject(projectDir, func(fset *token.FileSet, file *ast.File, src []byte, path string) {
		v := &closureVisitor{
			fset:      fset,
			src:       src,
			path:      path,
			goVersion: goVersion,
		}
		ast.Walk(v, file)
		allFindings = append(allFindings, v.findings...)
	})

	// Also check for goroutines closing over outer mutable vars
	// (pointers / maps / slices passed by reference without args)
	if err == nil {
		extra, err2 := checkOuterVarCaptures(projectDir)
		if err2 == nil {
			allFindings = append(allFindings, extra...)
		}
	}

	return allFindings, err
}

// checkOuterVarCaptures finds `go func()` (zero params) closures that reference
// pointer, slice, or map variables declared in the enclosing function scope.
func checkOuterVarCaptures(projectDir string) ([]ClosureFinding, error) {
	var findings []ClosureFinding

	walkProject(projectDir, func(fset *token.FileSet, file *ast.File, src []byte, path string) {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			// Collect locally declared pointer/map/slice vars in this function
			localPtrVars := collectMutableLocals(fn.Body)

			// Find goroutines with no params
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				goStmt, ok := n.(*ast.GoStmt)
				if !ok {
					return true
				}
				funcLit, ok := goStmt.Call.Fun.(*ast.FuncLit)
				if !ok {
					return true
				}
				// Only flag zero-parameter closures
				hasParams := funcLit.Type.Params != nil && len(funcLit.Type.Params.List) > 0
				if hasParams {
					return true
				}

				// Check which mutable outer vars are referenced
				goPos := fset.Position(goStmt.Pos())
				seenVars := make(map[string]bool)
				ast.Inspect(funcLit.Body, func(inner ast.Node) bool {
					ident, ok := inner.(*ast.Ident)
					if !ok {
						return true
					}
					if kind, isMutable := localPtrVars[ident.Name]; isMutable && !seenVars[ident.Name] {
						seenVars[ident.Name] = true
						identPos := fset.Position(ident.Pos())
						findings = append(findings, ClosureFinding{
							File:          path,
							Line:          identPos.Line,
							GoroutineLine: goPos.Line,
							VarName:       ident.Name,
							VarKind:       "outer_" + kind,
							LoopKind:      "none",
							FuncContext:   funcName(fn),
							Snippet:       snippetLines(src, fset, goStmt.Pos(), 3),
							Severity:      "WARNING",
							Issue: fmt.Sprintf("goroutine captures outer %s variable %q — "+
								"verify all concurrent accesses are synchronised", kind, ident.Name),
						})
					}
					return true
				})
				return true
			})
		}
	})

	return findings, nil
}

// collectMutableLocals returns a map of varName → kind ("pointer"|"map"|"slice")
// for variables declared in a function body that are pointer/map/slice types.
// We use a name-based heuristic: check the type expression of var declarations.
func collectMutableLocals(body *ast.BlockStmt) map[string]string {
	result := make(map[string]string)
	ast.Inspect(body, func(n ast.Node) bool {
		switch decl := n.(type) {
		case *ast.AssignStmt:
			if decl.Tok.String() == ":=" {
				for i, lhs := range decl.Lhs {
					if ident, ok := lhs.(*ast.Ident); ok && ident.Name != "_" {
						if i < len(decl.Rhs) {
							kind := mutableKind(decl.Rhs[i])
							if kind != "" {
								result[ident.Name] = kind
							}
						}
					}
				}
			}
		case *ast.DeclStmt:
			if genDecl, ok := decl.Decl.(*ast.GenDecl); ok {
				for _, spec := range genDecl.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for _, name := range vs.Names {
						if vs.Type != nil {
							kind := mutableKindFromExpr(vs.Type)
							if kind != "" {
								result[name.Name] = kind
							}
						}
					}
				}
			}
		}
		return true
	})
	return result
}

func mutableKind(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.UnaryExpr:
		if e.Op.String() == "&" {
			return "pointer"
		}
	case *ast.CompositeLit:
		switch e.Type.(type) {
		case *ast.MapType:
			return "map"
		case *ast.ArrayType:
			return "slice"
		}
	case *ast.CallExpr:
		if sel, ok := e.Fun.(*ast.Ident); ok && sel.Name == "make" && len(e.Args) > 0 {
			return mutableKindFromExpr(e.Args[0])
		}
	}
	return ""
}

func mutableKindFromExpr(expr ast.Expr) string {
	switch expr.(type) {
	case *ast.MapType:
		return "map"
	case *ast.ArrayType:
		return "slice"
	case *ast.StarExpr:
		return "pointer"
	}
	return ""
}

