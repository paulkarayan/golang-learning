package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/token"
	"os"
	"strings"
	"time"
)

// WGFinding represents a WaitGroup.Add call inside a goroutine body.
type WGFinding struct {
	File        string `json:"file"`
	Line        int    `json:"line"`
	WGName      string `json:"wg_name"`
	GoStmtLine  int    `json:"go_stmt_line"`
	FuncContext string `json:"func_context"`
	Snippet     string `json:"snippet"`
	Severity    string `json:"severity"`
	Issue       string `json:"issue"`
}

func runCheckWG(args []string) error {
	fs := flag.NewFlagSet("check-wg", flag.ExitOnError)
	dir := fs.String("dir", "", "project directory to analyse")
	output := fs.String("o", "", "output JSON file (default: auto-named)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dir == "" {
		return fmt.Errorf("-dir is required")
	}

	start := time.Now()
	findings, err := checkWG(*dir)
	if err != nil {
		return err
	}

	items := make([]json.RawMessage, 0, len(findings))
	for _, f := range findings {
		items = append(items, MarshalItem(f))
	}

	result := CheckResult{
		Check:      "check-wg",
		ProjectDir: absPath(*dir),
		RunAt:      start,
		DurationMs: time.Since(start).Milliseconds(),
		ItemCount:  len(items),
		Items:      items,
	}

	outPath := *output
	if outPath == "" {
		outPath = OutputFileName(*dir, "check-wg", start)
	}
	if err := WriteCheckResult(result, outPath); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "check-wg: %d finding(s) → %s\n", len(findings), outPath)
	return nil
}

func checkWG(projectDir string) ([]WGFinding, error) {
	var findings []WGFinding

	walkProject(projectDir, func(fset *token.FileSet, file *ast.File, src []byte, path string) {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}

			// For each go statement, walk the goroutine body for Add calls.
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				goStmt, ok := n.(*ast.GoStmt)
				if !ok {
					return true
				}
				goLine := fset.Position(goStmt.Pos()).Line

				ast.Inspect(goStmt.Call, func(inner ast.Node) bool {
					call, ok := inner.(*ast.CallExpr)
					if !ok {
						return true
					}
					sel, ok := call.Fun.(*ast.SelectorExpr)
					if !ok || sel.Sel.Name != "Add" {
						return true
					}
					// Heuristic: receiver name contains "wg", "WG", or "Wait"
					wgName := exprString(sel.X)
					if !isWGName(wgName) {
						return true
					}

					pos := fset.Position(call.Pos())
					findings = append(findings, WGFinding{
						File:        path,
						Line:        pos.Line,
						WGName:      wgName,
						GoStmtLine:  goLine,
						FuncContext: funcName(fn),
						Snippet:     snippetLines(src, fset, goStmt.Pos(), 4),
						Severity:    "BUG",
						Issue: fmt.Sprintf(
							"%s.Add() called inside goroutine body at line %d; "+
								"must be called before `go` to avoid race with wg.Wait()",
							wgName, pos.Line),
					})
					return true
				})
				return true
			})
		}
	})

	return findings, nil
}

// isWGName returns true if the name looks like a WaitGroup variable.
func isWGName(name string) bool {
	lower := strings.ToLower(name)
	return strings.Contains(lower, "wg") ||
		strings.Contains(lower, "wait") ||
		lower == "w"
}

// exprString returns a short textual representation of an AST expression.
func exprString(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return exprString(e.X) + "." + e.Sel.Name
	case *ast.StarExpr:
		return "*" + exprString(e.X)
	case *ast.ParenExpr:
		return exprString(e.X)
	case *ast.UnaryExpr:
		return e.Op.String() + exprString(e.X)
	}
	return "?"
}

// lineOf returns the source line of a token position.
func lineOf(fset *token.FileSet, pos token.Pos) int {
	return fset.Position(pos).Line
}
