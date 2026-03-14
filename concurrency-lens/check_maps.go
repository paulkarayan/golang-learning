package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// MapFinding represents a map written inside a goroutine without obvious protection.
type MapFinding struct {
	File          string       `json:"file"`
	Line          int          `json:"line"`
	MapName       string       `json:"map_name"`
	DeclaredFile  string       `json:"declared_file"`
	DeclaredLine  int          `json:"declared_line"`
	DeclaredScope string       `json:"declared_scope"` // package_var | struct_field | local
	WriteOp       string       `json:"write_op"`        // assign | delete
	GoroutineLine int          `json:"goroutine_line"`
	Protection    string       `json:"protection"` // none | mutex_nearby | rwmutex_nearby | sync_map
	Snippet       string       `json:"snippet"`
	Severity      string       `json:"severity"`
	Issue         string       `json:"issue"`
}

// mapDecl tracks a map declaration for cross-file correlation.
type mapDecl struct {
	name  string
	file  string
	line  int
	scope string // package_var | struct_field
}

func runCheckMaps(args []string) error {
	fs := flag.NewFlagSet("check-maps", flag.ExitOnError)
	dir := fs.String("dir", "", "project directory to analyse")
	output := fs.String("o", "", "output JSON file (default: auto-named)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dir == "" {
		return fmt.Errorf("-dir is required")
	}

	start := time.Now()
	findings, err := checkMaps(*dir)
	if err != nil {
		return err
	}

	items := make([]json.RawMessage, 0, len(findings))
	for _, f := range findings {
		items = append(items, MarshalItem(f))
	}

	result := CheckResult{
		Check:      "check-maps",
		ProjectDir: absPath(*dir),
		RunAt:      start,
		DurationMs: time.Since(start).Milliseconds(),
		ItemCount:  len(items),
		Items:      items,
	}

	outPath := *output
	if outPath == "" {
		outPath = OutputFileName(*dir, "check-maps", start)
	}
	if err := WriteCheckResult(result, outPath); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "check-maps: %d finding(s) → %s\n", len(findings), outPath)
	return nil
}

func checkMaps(projectDir string) ([]MapFinding, error) {
	// Pass 1: collect map declarations (package vars and struct fields).
	mapDecls := make(map[string]mapDecl) // key = varName or "TypeName.FieldName"

	walkProject(projectDir, func(fset *token.FileSet, file *ast.File, src []byte, path string) {
		rel, _ := filepath.Rel(projectDir, path)

		// Package-level var declarations
		for _, decl := range file.Decls {
			genDecl, ok := decl.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, spec := range genDecl.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, name := range vs.Names {
					if vs.Type != nil {
						if _, isMap := vs.Type.(*ast.MapType); isMap {
							pos := fset.Position(name.Pos())
							mapDecls[name.Name] = mapDecl{
								name:  name.Name,
								file:  rel,
								line:  pos.Line,
								scope: "package_var",
							}
						}
					}
					// Also catch var x = make(map[...])
					for _, val := range vs.Values {
						if isMapMake(val) {
							pos := fset.Position(name.Pos())
							mapDecls[name.Name] = mapDecl{
								name:  name.Name,
								file:  rel,
								line:  pos.Line,
								scope: "package_var",
							}
						}
					}
				}
			}

			// Struct fields
			for _, spec := range genDecl.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				st, ok := ts.Type.(*ast.StructType)
				if !ok {
					continue
				}
				for _, field := range st.Fields.List {
					if _, isMap := field.Type.(*ast.MapType); !isMap {
						continue
					}
					for _, fname := range field.Names {
						key := ts.Name.Name + "." + fname.Name
						pos := fset.Position(fname.Pos())
						mapDecls[key] = mapDecl{
							name:  fname.Name,
							file:  rel,
							line:  pos.Line,
							scope: "struct_field",
						}
					}
				}
			}
		}

		// Function-level make(map[...]) assignments
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				assign, ok := n.(*ast.AssignStmt)
				if !ok {
					return true
				}
				for i, rhs := range assign.Rhs {
					if isMapMake(rhs) && i < len(assign.Lhs) {
						if ident, ok := assign.Lhs[i].(*ast.Ident); ok {
							pos := fset.Position(ident.Pos())
							// Only track package-level-ish names (title-case or embedded in receiver)
							mapDecls[ident.Name] = mapDecl{
								name:  ident.Name,
								file:  rel,
								line:  pos.Line,
								scope: "local",
							}
						}
					}
				}
				return true
			})
		}
	})

	// Pass 2: find goroutines that write to any known map.
	var findings []MapFinding

	walkProject(projectDir, func(fset *token.FileSet, file *ast.File, src []byte, path string) {
		rel, _ := filepath.Rel(projectDir, path)

		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}

			// Find goroutine bodies in this function
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				goStmt, ok := n.(*ast.GoStmt)
				if !ok {
					return true
				}
				goLine := fset.Position(goStmt.Pos()).Line

				// Walk the goroutine body for map writes
				ast.Inspect(goStmt.Call, func(inner ast.Node) bool {
					switch stmt := inner.(type) {
					case *ast.AssignStmt:
						// Check: m[k] = v
						for _, lhs := range stmt.Lhs {
							idx, ok := lhs.(*ast.IndexExpr)
							if !ok {
								continue
							}
							mapName, decl, found := resolveMapName(idx.X, mapDecls, fn)
							if !found {
								return true
							}
							protection := detectLockProtection(fn.Body, fset, fset.Position(stmt.Pos()).Line)
							pos := fset.Position(stmt.Pos())
							findings = append(findings, MapFinding{
								File:          rel,
								Line:          pos.Line,
								MapName:       mapName,
								DeclaredFile:  decl.file,
								DeclaredLine:  decl.line,
								DeclaredScope: decl.scope,
								WriteOp:       "assign",
								GoroutineLine: goLine,
								Protection:    protection,
								Snippet:       snippetLines(src, fset, stmt.Pos(), 2),
								Severity:      severityFromProtection(protection),
								Issue:         issueFromProtection(mapName, protection),
							})
						}

					case *ast.ExprStmt:
						// Check: delete(m, k)
						call, ok := stmt.X.(*ast.CallExpr)
						if !ok {
							return true
						}
						fn2, ok := call.Fun.(*ast.Ident)
						if !ok || fn2.Name != "delete" || len(call.Args) < 1 {
							return true
						}
						mapName, decl, found := resolveMapName(call.Args[0], mapDecls, fn)
						if !found {
							return true
						}
						protection := detectLockProtection(fn.Body, fset, fset.Position(stmt.Pos()).Line)
						pos := fset.Position(stmt.Pos())
						findings = append(findings, MapFinding{
							File:          rel,
							Line:          pos.Line,
							MapName:       mapName,
							DeclaredFile:  decl.file,
							DeclaredLine:  decl.line,
							DeclaredScope: decl.scope,
							WriteOp:       "delete",
							GoroutineLine: goLine,
							Protection:    protection,
							Snippet:       snippetLines(src, fset, stmt.Pos(), 2),
							Severity:      severityFromProtection(protection),
							Issue:         issueFromProtection(mapName, protection),
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

// resolveMapName tries to determine the map's declaration name from an AST expression.
func resolveMapName(expr ast.Expr, decls map[string]mapDecl, fn *ast.FuncDecl) (string, mapDecl, bool) {
	switch e := expr.(type) {
	case *ast.Ident:
		if d, ok := decls[e.Name]; ok {
			return e.Name, d, true
		}
	case *ast.SelectorExpr:
		// s.stations, s.cache, etc.
		// key = ReceiverType.FieldName
		if fn.Recv != nil && len(fn.Recv.List) > 0 {
			recvType := receiverTypeName(fn)
			key := recvType + "." + e.Sel.Name
			if d, ok := decls[key]; ok {
				return e.Sel.Name, d, true
			}
		}
		// Also try field name alone
		if d, ok := decls[e.Sel.Name]; ok {
			return e.Sel.Name, d, true
		}
	}
	return "", mapDecl{}, false
}

// detectLockProtection looks for a Lock/RLock call in the same function body
// within a window of lines around writeLine.
func detectLockProtection(body *ast.BlockStmt, fset *token.FileSet, writeLine int) string {
	protection := "none"
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pos := fset.Position(call.Pos()).Line
		// Look within ±20 lines of the write
		if abs(pos-writeLine) > 20 {
			return true
		}
		switch sel.Sel.Name {
		case "Lock":
			protection = "mutex_nearby"
		case "RLock":
			if protection != "mutex_nearby" {
				protection = "rwmutex_nearby"
			}
		}
		// Check if using sync.Map
		if ident, ok := sel.X.(*ast.Ident); ok {
			if strings.Contains(strings.ToLower(ident.Name), "sync") {
				protection = "sync_map"
			}
		}
		return true
	})
	return protection
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func severityFromProtection(p string) string {
	if p == "none" {
		return "BUG"
	}
	return "WARNING"
}

func issueFromProtection(mapName, p string) string {
	if p == "none" {
		return fmt.Sprintf("map %q written inside goroutine without any lock", mapName)
	}
	return fmt.Sprintf("map %q written inside goroutine; a lock is nearby but may not cover this write", mapName)
}

func isMapMake(expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	fn, ok := call.Fun.(*ast.Ident)
	if !ok || fn.Name != "make" {
		return false
	}
	if len(call.Args) == 0 {
		return false
	}
	_, isMap := call.Args[0].(*ast.MapType)
	return isMap
}
