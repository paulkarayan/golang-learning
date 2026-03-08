package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// SemgrepResult represents the top-level semgrep JSON output.
type SemgrepResult struct {
	Results []SemgrepFinding `json:"results"`
}

// SemgrepFinding represents a single semgrep finding.
type SemgrepFinding struct {
	CheckID string `json:"check_id"`
	Path    string `json:"path"`
	Start   struct {
		Line int `json:"line"`
		Col  int `json:"col"`
	} `json:"start"`
	End struct {
		Line int `json:"line"`
		Col  int `json:"col"`
	} `json:"end"`
	Extra struct {
		Message  string `json:"message"`
		Severity string `json:"severity"`
	} `json:"extra"`
}

// TriagePrompt is the output of the triage step — a targeted LLM prompt
// with its source context.
type TriagePrompt struct {
	RuleID  string `json:"rule_id"`
	File    string `json:"file"`
	Line    int    `json:"line"`
	Context string `json:"context"`
	Prompt  string `json:"prompt"`
}

func runTriage(args []string) error {
	fs := flag.NewFlagSet("triage", flag.ExitOnError)
	semgrepFile := fs.String("semgrep", "", "path to semgrep JSON output file")
	projectDir := fs.String("dir", ".", "project directory to analyze")
	outputFile := fs.String("o", "", "output file for prompts JSON (default: stdout)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *semgrepFile == "" {
		return fmt.Errorf("-semgrep flag is required")
	}

	// Parse semgrep results
	findings, err := parseSemgrepJSON(*semgrepFile)
	if err != nil {
		return fmt.Errorf("parsing semgrep JSON: %w", err)
	}

	var prompts []TriagePrompt

	// Process semgrep findings, skipping test files
	for _, f := range findings {
		if strings.HasSuffix(f.Path, "_test.go") {
			continue
		}
		p, err := processFinding(f, *projectDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: skipping %s:%d (%s): %v\n", f.Path, f.Start.Line, f.CheckID, err)
			continue
		}
		if p != nil {
			prompts = append(prompts, *p)
		}
	}

	// Run grep-based scans for patterns without semgrep rules
	grepPrompts, err := grepBasedScans(*projectDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: grep-based scans failed: %v\n", err)
	} else {
		prompts = append(prompts, grepPrompts...)
	}

	// Deduplicate: if mutex-lock-inventory has fewer than 2 findings per type, drop them
	prompts = filterMutexFindings(prompts)

	// Output
	out := os.Stdout
	if *outputFile != "" {
		f, err := os.Create(*outputFile)
		if err != nil {
			return err
		}
		defer f.Close()
		out = f
	}

	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(prompts)
}

func parseSemgrepJSON(path string) ([]SemgrepFinding, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var result SemgrepResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result.Results, nil
}

// processFinding extracts source context for a semgrep finding using go/ast
// and builds a targeted LLM prompt.
func processFinding(f SemgrepFinding, projectDir string) (*TriagePrompt, error) {
	filePath := filepath.Join(projectDir, f.Path)

	switch f.CheckID {
	case "closeable-type-inventory":
		ctx, err := extractTypeAndMethods(filePath, f.Start.Line)
		if err != nil {
			return nil, err
		}
		return &TriagePrompt{
			RuleID:  f.CheckID,
			File:    f.Path,
			Line:    f.Start.Line,
			Context: ctx,
			Prompt:  buildPrompt(f.CheckID, ctx),
		}, nil

	case "channel-make-inventory":
		ctx, err := extractEnclosingFunction(filePath, f.Start.Line)
		if err != nil {
			return nil, err
		}
		return &TriagePrompt{
			RuleID:  f.CheckID,
			File:    f.Path,
			Line:    f.Start.Line,
			Context: ctx,
			Prompt:  buildPrompt(f.CheckID, ctx),
		}, nil

	case "mutex-lock-inventory":
		ctx, err := extractEnclosingFunction(filePath, f.Start.Line)
		if err != nil {
			return nil, err
		}
		return &TriagePrompt{
			RuleID:  f.CheckID,
			File:    f.Path,
			Line:    f.Start.Line,
			Context: ctx,
			Prompt:  buildPrompt(f.CheckID, ctx),
		}, nil

	case "defer-channel-send", "fire-and-forget-goroutine",
		"wg-add-inside-goroutine", "channel-close-without-once":
		ctx, err := extractEnclosingFunction(filePath, f.Start.Line)
		if err != nil {
			return nil, err
		}
		return &TriagePrompt{
			RuleID:  f.CheckID,
			File:    f.Path,
			Line:    f.Start.Line,
			Context: ctx,
			Prompt:  buildPrompt(f.CheckID, ctx),
		}, nil

	default:
		// Unknown rule — extract function context and use generic prompt
		ctx, err := extractEnclosingFunction(filePath, f.Start.Line)
		if err != nil {
			return nil, err
		}
		return &TriagePrompt{
			RuleID:  f.CheckID,
			File:    f.Path,
			Line:    f.Start.Line,
			Context: ctx,
			Prompt:  buildPrompt(f.CheckID, ctx),
		}, nil
	}
}

// extractEnclosingFunction uses go/ast to find the function containing the
// given line and returns its source text with file/line annotations.
func extractEnclosingFunction(filePath string, line int) (string, error) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		return "", fmt.Errorf("parsing %s: %w", filePath, err)
	}

	src, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}

	for _, decl := range node.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		startLine := fset.Position(fn.Pos()).Line
		endLine := fset.Position(fn.End()).Line
		if line >= startLine && line <= endLine {
			return formatSourceRange(filePath, src, startLine, endLine), nil
		}
	}

	// Fallback: return surrounding lines
	return formatSourceRange(filePath, src, max(1, line-5), line+20), nil
}

// extractTypeAndMethods finds the receiver type of the method at the given line,
// then extracts ALL methods on that type from the same file.
func extractTypeAndMethods(filePath string, line int) (string, error) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		return "", fmt.Errorf("parsing %s: %w", filePath, err)
	}

	src, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}

	// Find the receiver type name at the given line
	var receiverType string
	for _, decl := range node.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv == nil {
			continue
		}
		startLine := fset.Position(fn.Pos()).Line
		endLine := fset.Position(fn.End()).Line
		if line >= startLine && line <= endLine {
			receiverType = receiverTypeName(fn)
			break
		}
	}

	if receiverType == "" {
		return extractEnclosingFunction(filePath, line)
	}

	// Find the type definition
	var parts []string
	for _, decl := range node.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.TYPE {
			continue
		}
		for _, spec := range genDecl.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok || ts.Name.Name != receiverType {
				continue
			}
			start := fset.Position(genDecl.Pos()).Line
			end := fset.Position(genDecl.End()).Line
			parts = append(parts, formatSourceRange(filePath, src, start, end))
		}
	}

	// Collect all methods on this receiver type
	for _, decl := range node.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv == nil {
			continue
		}
		if receiverTypeName(fn) == receiverType {
			start := fset.Position(fn.Pos()).Line
			end := fset.Position(fn.End()).Line
			parts = append(parts, formatSourceRange(filePath, src, start, end))
		}
	}

	// Also scan other .go files in the same directory for methods on this type
	dir := filepath.Dir(filePath)
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		otherPath := filepath.Join(dir, entry.Name())
		if otherPath == filePath {
			continue
		}
		otherParts, _ := extractMethodsOnType(otherPath, receiverType)
		parts = append(parts, otherParts...)
	}

	if len(parts) == 0 {
		return extractEnclosingFunction(filePath, line)
	}

	return strings.Join(parts, "\n\n"), nil
}

// extractMethodsOnType returns source for all methods on a given receiver type.
func extractMethodsOnType(filePath, typeName string) ([]string, error) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	src, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	var parts []string
	for _, decl := range node.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv == nil {
			continue
		}
		if receiverTypeName(fn) == typeName {
			start := fset.Position(fn.Pos()).Line
			end := fset.Position(fn.End()).Line
			parts = append(parts, formatSourceRange(filePath, src, start, end))
		}
	}
	return parts, nil
}

// receiverTypeName extracts the type name from a method receiver.
func receiverTypeName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return ""
	}
	typ := fn.Recv.List[0].Type
	// Unwrap pointer
	if star, ok := typ.(*ast.StarExpr); ok {
		typ = star.X
	}
	if ident, ok := typ.(*ast.Ident); ok {
		return ident.Name
	}
	return ""
}

// formatSourceRange extracts lines [start, end] from src and formats with
// file path and line numbers.
func formatSourceRange(filePath string, src []byte, start, end int) string {
	lines := strings.Split(string(src), "\n")
	if start < 1 {
		start = 1
	}
	if end > len(lines) {
		end = len(lines)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "// %s:%d-%d\n", filePath, start, end)
	for i := start; i <= end; i++ {
		fmt.Fprintf(&b, "%4d | %s\n", i, lines[i-1])
	}
	return b.String()
}

// grepBasedScans looks for patterns that semgrep can't detect well:
// TOCTOU map lookups and context propagation issues.
func grepBasedScans(projectDir string) ([]TriagePrompt, error) {
	var prompts []TriagePrompt

	// TOCTOU: look for patterns like "v := map[key]" or ".Get(" followed by use
	toctouPrompts, err := scanForTOCTOU(projectDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: TOCTOU scan failed: %v\n", err)
	} else {
		prompts = append(prompts, toctouPrompts...)
	}

	// Context propagation: functions with blocking ops that don't take context
	ctxPrompts, err := scanForContextPropagation(projectDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: context propagation scan failed: %v\n", err)
	} else {
		prompts = append(prompts, ctxPrompts...)
	}

	return prompts, nil
}

// scanForTOCTOU uses grep to find map lookups or .Get() calls in non-test Go files,
// then extracts the enclosing function for LLM analysis.
func scanForTOCTOU(projectDir string) ([]TriagePrompt, error) {
	// Look for Get/Load patterns on sync.Map or custom stores
	// Use sh -c because glob patterns in --include/--exclude need shell expansion
	cmd := exec.Command("sh", "-c",
		fmt.Sprintf("grep -rn --include='*.go' --exclude='*_test.go' '\\.Get(' %s", projectDir))
	output, err := cmd.Output()
	if err != nil {
		// grep returns exit 1 for no matches
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return nil, nil
		}
		return nil, err
	}

	seen := make(map[string]bool)
	var prompts []TriagePrompt
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, ":", 3)
		if len(parts) < 3 {
			continue
		}
		file := parts[0]
		lineNum := 0
		fmt.Sscanf(parts[1], "%d", &lineNum)

		// Deduplicate by file+function
		key := fmt.Sprintf("%s:%d", file, lineNum)
		if seen[key] {
			continue
		}
		seen[key] = true

		ctx, err := extractEnclosingFunction(file, lineNum)
		if err != nil {
			continue
		}

		relPath, _ := filepath.Rel(projectDir, file)
		if relPath == "" {
			relPath = file
		}

		prompts = append(prompts, TriagePrompt{
			RuleID:  "toctou-map-lookup",
			File:    relPath,
			Line:    lineNum,
			Context: ctx,
			Prompt:  buildPrompt("toctou-map-lookup", ctx),
		})
	}

	return prompts, nil
}

// scanForContextPropagation finds functions with blocking operations
// (channel ops, I/O) that don't accept context.Context.
func scanForContextPropagation(projectDir string) ([]TriagePrompt, error) {
	// Find functions containing select statements or channel operations
	cmd := exec.Command("sh", "-c",
		fmt.Sprintf("grep -rn --include='*.go' --exclude='*_test.go' 'select {' %s", projectDir))
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return nil, nil
		}
		return nil, err
	}

	seen := make(map[string]bool)
	var prompts []TriagePrompt
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, ":", 3)
		if len(parts) < 3 {
			continue
		}
		file := parts[0]
		lineNum := 0
		fmt.Sscanf(parts[1], "%d", &lineNum)

		key := fmt.Sprintf("%s:%d", file, lineNum)
		if seen[key] {
			continue
		}
		seen[key] = true

		ctx, err := extractEnclosingFunction(file, lineNum)
		if err != nil {
			continue
		}

		relPath, _ := filepath.Rel(projectDir, file)
		if relPath == "" {
			relPath = file
		}

		prompts = append(prompts, TriagePrompt{
			RuleID:  "context-propagation",
			File:    relPath,
			Line:    lineNum,
			Context: ctx,
			Prompt:  buildPrompt("context-propagation", ctx),
		})
	}

	return prompts, nil
}

// filterMutexFindings drops mutex-lock-inventory prompts unless there are 2+
// lock sites on the same type (single lock can't have ordering issues).
func filterMutexFindings(prompts []TriagePrompt) []TriagePrompt {
	// Count mutex findings per file
	mutexCounts := make(map[string]int)
	for _, p := range prompts {
		if p.RuleID == "mutex-lock-inventory" {
			mutexCounts[p.File]++
		}
	}

	var filtered []TriagePrompt
	for _, p := range prompts {
		if p.RuleID == "mutex-lock-inventory" && mutexCounts[p.File] < 2 {
			continue
		}
		filtered = append(filtered, p)
	}
	return filtered
}
