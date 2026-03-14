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

// OwnershipItem represents a variable or struct field with its ownership analysis.
type OwnershipItem struct {
	Name            string `json:"name"`             // e.g. "Server.stations" or "globalCache"
	Kind            string `json:"kind"`             // struct_field | package_var
	TypeStr         string `json:"type_str"`
	File            string `json:"file"`
	Line            int    `json:"line"`
	IsSyncPrimitive bool   `json:"is_sync_primitive"` // mutex, channel, wg, atomic, once
	AccessPattern   string `json:"access_pattern"`    // goroutine_write | goroutine_read | single_goroutine | unknown
	// LLM-populated fields
	Owner         string `json:"owner"`
	Protection    string `json:"protection"`
	CommentClaim  string `json:"comment_claim,omitempty"` // what comments say — for user to verify
	Status        string `json:"status"`                  // green | yellow | red
	StatusReason  string `json:"status_reason"`
	LLMConfidence string `json:"llm_confidence"` // high | medium | low
	LLMRaw        string `json:"llm_raw,omitempty"`
}

func runCheckOwnership(args []string) error {
	fs := flag.NewFlagSet("check-ownership", flag.ExitOnError)
	dir := fs.String("dir", "", "project directory to analyse")
	output := fs.String("o", "", "output JSON file (default: auto-named)")
	noLLM := fs.Bool("no-llm", false, "skip LLM annotation (AST only)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dir == "" {
		return fmt.Errorf("-dir is required")
	}

	start := time.Now()
	items, err := checkOwnership(*dir, !*noLLM)
	if err != nil {
		return err
	}

	raw := make([]json.RawMessage, 0, len(items))
	for _, it := range items {
		raw = append(raw, MarshalItem(it))
	}

	result := CheckResult{
		Check:      "check-ownership",
		ProjectDir: absPath(*dir),
		RunAt:      start,
		DurationMs: time.Since(start).Milliseconds(),
		ItemCount:  len(raw),
		Items:      raw,
	}

	outPath := *output
	if outPath == "" {
		outPath = OutputFileName(*dir, "check-ownership", start)
	}
	if err := WriteCheckResult(result, outPath); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "check-ownership: %d item(s) → %s\n", len(items), outPath)
	return nil
}

func checkOwnership(projectDir string, useLLM bool) ([]OwnershipItem, error) {
	// Phase 1: AST discovery of all struct fields and package-level vars.
	items := discoverOwnershipCandidates(projectDir)

	if !useLLM {
		return items, nil
	}

	// Phase 2: for each non-trivial item, call LLM to annotate ownership.
	// Group by file to give the LLM enough context.
	byFile := make(map[string][]*OwnershipItem)
	for i := range items {
		it := &items[i]
		if it.IsSyncPrimitive {
			// Sync primitives annotate themselves.
			it.Status = "green"
			it.StatusReason = "sync primitive — is its own protection mechanism"
			it.LLMConfidence = "high"
			continue
		}
		byFile[it.File] = append(byFile[it.File], it)
	}

	for file, fileItems := range byFile {
		if err := annotateOwnershipForFile(projectDir, file, fileItems); err != nil {
			fmt.Fprintf(os.Stderr, "warning: LLM annotation failed for %s: %v\n", file, err)
			for _, it := range fileItems {
				if it.Status == "" {
					it.Status = "yellow"
					it.StatusReason = "LLM annotation failed — manual review needed"
					it.LLMConfidence = "low"
				}
			}
		}
	}

	return items, nil
}

func discoverOwnershipCandidates(projectDir string) []OwnershipItem {
	var items []OwnershipItem

	walkProject(projectDir, func(fset *token.FileSet, file *ast.File, src []byte, path string) {
		rel, _ := filepath.Rel(projectDir, path)

		// Package-level vars
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
					if name.Name == "_" {
						continue
					}
					pos := fset.Position(name.Pos())
					typeStr := typeExprString(vs.Type)
					items = append(items, OwnershipItem{
						Name:            name.Name,
						Kind:            "package_var",
						TypeStr:         typeStr,
						File:            rel,
						Line:            pos.Line,
						IsSyncPrimitive: isSyncType(typeStr),
						AccessPattern:   "unknown",
					})
				}
			}
		}

		// Struct fields
		for _, decl := range file.Decls {
			genDecl, ok := decl.(*ast.GenDecl)
			if !ok {
				continue
			}
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
					typeStr := typeExprString(field.Type)
					for _, fname := range field.Names {
						pos := fset.Position(fname.Pos())
						items = append(items, OwnershipItem{
							Name:            ts.Name.Name + "." + fname.Name,
							Kind:            "struct_field",
							TypeStr:         typeStr,
							File:            rel,
							Line:            pos.Line,
							IsSyncPrimitive: isSyncType(typeStr),
							AccessPattern:   "unknown",
						})
					}
				}
			}
		}
	})

	// Phase 1b: mark access patterns by scanning for goroutine writes.
	markAccessPatterns(projectDir, items)

	return items
}

func markAccessPatterns(projectDir string, items []OwnershipItem) {
	writtenInGoroutine := make(map[string]bool)
	readInGoroutine := make(map[string]bool)

	// Build index of all function bodies so we can follow named goroutine calls.
	funcIndex := buildFuncBodyIndex(projectDir)

	walkProject(projectDir, func(fset *token.FileSet, file *ast.File, src []byte, path string) {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				goStmt, ok := n.(*ast.GoStmt)
				if !ok {
					return true
				}
				scanGoroutineEffectiveBody(goStmt, funcIndex, writtenInGoroutine, readInGoroutine)
				return true
			})
		}
	})

	for i := range items {
		it := &items[i]
		shortName := shortFieldName(it.Name)
		if writtenInGoroutine[shortName] || writtenInGoroutine[it.Name] {
			it.AccessPattern = "goroutine_write"
		} else if readInGoroutine[shortName] || readInGoroutine[it.Name] {
			it.AccessPattern = "goroutine_read"
		}
	}
}

// buildFuncBodyIndex indexes all function declarations by name for named-goroutine resolution.
func buildFuncBodyIndex(projectDir string) map[string][]*ast.FuncDecl {
	index := make(map[string][]*ast.FuncDecl)
	walkProject(projectDir, func(_ *token.FileSet, file *ast.File, _ []byte, _ string) {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			index[fn.Name.Name] = append(index[fn.Name.Name], fn)
		}
	})
	return index
}

// scanGoroutineEffectiveBody collects read/write idents from the goroutine's effective body.
// For inline closures it scans the literal. For named calls (go f() / go obj.M()) it
// resolves one level deep into the named function's body via the index.
func scanGoroutineEffectiveBody(goStmt *ast.GoStmt, funcIndex map[string][]*ast.FuncDecl, written, read map[string]bool) {
	var nodesToScan []ast.Node

	switch fun := goStmt.Call.Fun.(type) {
	case *ast.FuncLit:
		// Inline closure — the call expression itself contains the body.
		nodesToScan = append(nodesToScan, goStmt.Call)
	case *ast.Ident:
		// go namedFunc(args)
		for _, fn := range funcIndex[fun.Name] {
			nodesToScan = append(nodesToScan, fn.Body)
		}
	case *ast.SelectorExpr:
		// go obj.Method(args)
		for _, fn := range funcIndex[fun.Sel.Name] {
			nodesToScan = append(nodesToScan, fn.Body)
		}
	}

	for _, node := range nodesToScan {
		ast.Inspect(node, func(inner ast.Node) bool {
			switch stmt := inner.(type) {
			case *ast.AssignStmt:
				for _, lhs := range stmt.Lhs {
					collectIdents(lhs, written)
				}
				for _, rhs := range stmt.Rhs {
					collectIdents(rhs, read)
				}
			case *ast.SelectorExpr:
				read[exprString(stmt)] = true
			}
			return true
		})
	}
}

func collectIdents(expr ast.Expr, set map[string]bool) {
	switch e := expr.(type) {
	case *ast.Ident:
		set[e.Name] = true
	case *ast.SelectorExpr:
		set[exprString(e)] = true
		set[e.Sel.Name] = true
	case *ast.IndexExpr:
		collectIdents(e.X, set)
	case *ast.StarExpr:
		collectIdents(e.X, set)
	}
}

func shortFieldName(name string) string {
	if idx := strings.LastIndex(name, "."); idx >= 0 {
		return name[idx+1:]
	}
	return name
}

// annotateOwnershipForFile calls the LLM once per file with all non-trivial items.
func annotateOwnershipForFile(projectDir, relFile string, items []*OwnershipItem) error {
	if len(items) == 0 {
		return nil
	}

	// Read the file for context
	fullPath := filepath.Join(projectDir, relFile)
	src, err := os.ReadFile(fullPath)
	if err != nil {
		return err
	}
	// Truncate large files to avoid token limits
	srcStr := string(src)
	if len(srcStr) > 8000 {
		srcStr = srcStr[:8000] + "\n// ... (truncated)"
	}

	// Build item list for the prompt
	var itemLines strings.Builder
	for _, it := range items {
		fmt.Fprintf(&itemLines, "- %s (type: %s, line: %d, access: %s)\n",
			it.Name, it.TypeStr, it.Line, it.AccessPattern)
	}

	prompt := fmt.Sprintf(`You are auditing Go code for data ownership and race conditions.

IMPORTANT: Do NOT treat code comments or documentation as evidence of correct synchronisation.
Comments describe intent — the bug you are looking for is precisely when comments claim protection
but code does not enforce it consistently. Note what comments claim in "comment_claim" so the
user can verify, but base "status" and "protection" only on what the code actually does.

File: %s

Source:
%s

Variables/fields to analyse:
%s

For each variable/field:
1. Find every place it is READ or WRITTEN in the source (note line numbers).
2. Check whether a mutex/channel/atomic is demonstrably held at EVERY such site.
3. Note what nearby comments CLAIM about protection (for the user to verify separately).
4. Assign status:
   - "green": every access site is demonstrably protected by a consistent sync mechanism
   - "yellow": some access sites are unclear, file is truncated, or protection is partial
   - "red": found at least one access site without apparent protection, or conflicting patterns

Respond with a JSON array, one object per variable in the same order:
[
  {
    "name": "<exact name from list>",
    "owner": "<owner description>",
    "protection": "<sync mechanism observed in code, or 'none' or 'unverified'>",
    "comment_claim": "<what comments say about protection, or 'none'>",
    "status": "green|yellow|red",
    "status_reason": "<one sentence referencing specific line numbers or access sites>",
    "llm_confidence": "high|medium|low"
  }
]

JSON only, no markdown.`, relFile, srcStr, itemLines.String())

	type llmItem struct {
		Name          string `json:"name"`
		Owner         string `json:"owner"`
		Protection    string `json:"protection"`
		CommentClaim  string `json:"comment_claim"`
		Status        string `json:"status"`
		StatusReason  string `json:"status_reason"`
		LLMConfidence string `json:"llm_confidence"`
	}
	var results []llmItem
	if err := callLLMForJSON(prompt, &results); err != nil {
		return err
	}

	// Match results back to items by name
	resultByName := make(map[string]llmItem)
	for _, r := range results {
		resultByName[r.Name] = r
	}
	for _, it := range items {
		if r, ok := resultByName[it.Name]; ok {
			it.Owner = r.Owner
			it.Protection = r.Protection
			it.CommentClaim = r.CommentClaim
			it.Status = r.Status
			it.StatusReason = r.StatusReason
			it.LLMConfidence = r.LLMConfidence
		} else {
			it.Status = "yellow"
			it.StatusReason = "LLM did not return result for this item"
			it.LLMConfidence = "low"
		}
	}
	return nil
}

// isSyncType returns true for sync package types and channel/atomic types.
func isSyncType(typeStr string) bool {
	syncs := []string{
		"sync.Mutex", "sync.RWMutex", "sync.WaitGroup", "sync.Once",
		"sync.Cond", "sync.Map", "atomic.",
		// bare names (via dot-import or type alias)
		"Mutex", "RWMutex", "WaitGroup", "Once", "Cond",
		// channels
		"chan ", "chan<-", "<-chan",
	}
	lower := strings.ToLower(typeStr)
	for _, s := range syncs {
		if strings.Contains(lower, strings.ToLower(s)) {
			return true
		}
	}
	return false
}

// typeExprString returns a string representation of a type expression.
func typeExprString(expr ast.Expr) string {
	if expr == nil {
		return "unknown"
	}
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.StarExpr:
		return "*" + typeExprString(e.X)
	case *ast.ArrayType:
		if e.Len == nil {
			return "[]" + typeExprString(e.Elt)
		}
		return "[...]" + typeExprString(e.Elt)
	case *ast.MapType:
		return "map[" + typeExprString(e.Key) + "]" + typeExprString(e.Value)
	case *ast.SelectorExpr:
		return typeExprString(e.X) + "." + e.Sel.Name
	case *ast.ChanType:
		switch e.Dir {
		case ast.SEND:
			return "chan<- " + typeExprString(e.Value)
		case ast.RECV:
			return "<-chan " + typeExprString(e.Value)
		default:
			return "chan " + typeExprString(e.Value)
		}
	case *ast.InterfaceType:
		return "interface{}"
	case *ast.FuncType:
		return "func(...)"
	case *ast.StructType:
		return "struct{...}"
	case *ast.Ellipsis:
		return "..." + typeExprString(e.Elt)
	}
	return "?"
}
