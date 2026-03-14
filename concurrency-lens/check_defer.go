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

// DeferUnlockFinding is a defer mu.Unlock() inside a for loop body.
type DeferUnlockFinding struct {
	File        string `json:"file"`
	Line        int    `json:"line"`
	MutexName   string `json:"mutex_name"`
	UnlockKind  string `json:"unlock_kind"` // Unlock | RUnlock | Done
	LoopLine    int    `json:"loop_line"`
	LoopKind    string `json:"loop_kind"` // for_range | for_classic | for_condition
	FuncContext string `json:"func_context"`
	Snippet     string `json:"snippet"`
	Severity    string `json:"severity"`
	Issue       string `json:"issue"`
}

func runCheckDefer(args []string) error {
	fs := flag.NewFlagSet("check-defer-unlock", flag.ExitOnError)
	dir := fs.String("dir", "", "project directory to analyse")
	output := fs.String("o", "", "output JSON file (default: auto-named)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dir == "" {
		return fmt.Errorf("-dir is required")
	}

	start := time.Now()
	findings, err := checkDefer(*dir)
	if err != nil {
		return err
	}

	items := make([]json.RawMessage, 0, len(findings))
	for _, f := range findings {
		items = append(items, MarshalItem(f))
	}

	result := CheckResult{
		Check:      "check-defer-unlock",
		ProjectDir: absPath(*dir),
		RunAt:      start,
		DurationMs: time.Since(start).Milliseconds(),
		ItemCount:  len(items),
		Items:      items,
	}

	outPath := *output
	if outPath == "" {
		outPath = OutputFileName(*dir, "check-defer-unlock", start)
	}
	if err := WriteCheckResult(result, outPath); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "check-defer-unlock: %d finding(s) → %s\n", len(findings), outPath)
	return nil
}

func checkDefer(projectDir string) ([]DeferUnlockFinding, error) {
	var findings []DeferUnlockFinding

	walkProject(projectDir, func(fset *token.FileSet, file *ast.File, src []byte, path string) {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			findings = append(findings, findDeferUnlocksInLoops(fset, fn, src, path)...)
		}
	})

	return findings, nil
}

// findDeferUnlocksInLoops finds defer Unlock/RUnlock/Done calls inside for loops.
func findDeferUnlocksInLoops(fset *token.FileSet, fn *ast.FuncDecl, src []byte, path string) []DeferUnlockFinding {
	var findings []DeferUnlockFinding

	// Walk the function, tracking whether we're inside a for/range loop.
	// We use a recursive approach to handle nested loops correctly.
	var walkBody func(stmts []ast.Stmt, inLoop bool, loopLine int, loopKind string)
	walkBody = func(stmts []ast.Stmt, inLoop bool, loopLine int, loopKind string) {
		for _, stmt := range stmts {
			switch s := stmt.(type) {
			case *ast.ForStmt:
				body := s.Body.List
				line := fset.Position(s.Pos()).Line
				kind := "for_classic"
				if s.Cond != nil && s.Init == nil {
					kind = "for_condition"
				}
				walkBody(body, true, line, kind)

			case *ast.RangeStmt:
				body := s.Body.List
				line := fset.Position(s.Pos()).Line
				walkBody(body, true, line, "for_range")

			case *ast.DeferStmt:
				if !inLoop {
					continue
				}
				// Check if the deferred call is Unlock, RUnlock, or Done
				call, ok := s.Call.Fun.(*ast.SelectorExpr)
				if !ok {
					continue
				}
				kind := ""
				switch call.Sel.Name {
				case "Unlock":
					kind = "Unlock"
				case "RUnlock":
					kind = "RUnlock"
				case "Done":
					kind = "Done"
				}
				if kind == "" {
					continue
				}
				mutexName := exprString(call.X)
				pos := fset.Position(s.Pos())
				findings = append(findings, DeferUnlockFinding{
					File:        path,
					Line:        pos.Line,
					MutexName:   mutexName,
					UnlockKind:  kind,
					LoopLine:    loopLine,
					LoopKind:    loopKind,
					FuncContext: funcName(fn),
					Snippet:     snippetLines(src, fset, s.Pos(), 3),
					Severity:    "BUG",
					Issue: fmt.Sprintf(
						"defer %s.%s() inside %s loop (line %d) will not fire until %s() returns, "+
							"holding the lock for the entire duration",
						mutexName, kind, loopKind, loopLine, funcName(fn)),
				})

			case *ast.BlockStmt:
				walkBody(s.List, inLoop, loopLine, loopKind)

			case *ast.IfStmt:
				var ifStmts []ast.Stmt
				if s.Body != nil {
					ifStmts = append(ifStmts, s.Body.List...)
				}
				if s.Else != nil {
					switch e := s.Else.(type) {
					case *ast.BlockStmt:
						ifStmts = append(ifStmts, e.List...)
					case *ast.IfStmt:
						ifStmts = append(ifStmts, e)
					}
				}
				walkBody(ifStmts, inLoop, loopLine, loopKind)

			case *ast.SwitchStmt:
				if s.Body != nil {
					walkBody(s.Body.List, inLoop, loopLine, loopKind)
				}

			case *ast.SelectStmt:
				if s.Body != nil {
					walkBody(s.Body.List, inLoop, loopLine, loopKind)
				}

			case *ast.CommClause:
				walkBody(s.Body, inLoop, loopLine, loopKind)

			case *ast.CaseClause:
				walkBody(s.Body, inLoop, loopLine, loopKind)
			}
		}
	}

	walkBody(fn.Body.List, false, 0, "")
	return findings
}

