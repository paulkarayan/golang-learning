package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// walkProject calls fn for every non-test .go file in dir (recursive).
// Skips vendor/, testdata/, and .git/ directories.
func walkProject(dir string, fn func(fset *token.FileSet, file *ast.File, src []byte, path string)) error {
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			name := info.Name()
			if name == "vendor" || name == "testdata" || name == ".git" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return nil // skip unreadable files
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, src, parser.ParseComments)
		if err != nil {
			return nil // skip files with parse errors
		}
		fn(fset, file, src, path)
		return nil
	})
}

// formatSourceRange returns lines [start,end] from src, annotated with file:line.
func formatSourceRange(filePath string, src []byte, start, end int) string {
	lines := strings.Split(string(src), "\n")
	start = max(start, 1)
	end = min(end, len(lines))
	var b strings.Builder
	fmt.Fprintf(&b, "// %s:%d-%d\n", filePath, start, end)
	for i := start; i <= end; i++ {
		fmt.Fprintf(&b, "%4d | %s\n", i, lines[i-1])
	}
	return b.String()
}

// enclosingFunction returns the function declaration that contains the given line,
// or a fallback ±10 lines if no function is found.
func enclosingFunction(fset *token.FileSet, file *ast.File, src []byte, filePath string, line int) string {
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		startLine := fset.Position(fn.Pos()).Line
		endLine := fset.Position(fn.End()).Line
		if line >= startLine && line <= endLine {
			return formatSourceRange(filePath, src, startLine, endLine)
		}
	}
	// fallback
	maxLine := len(strings.Split(string(src), "\n"))
	start := max(line-5, 1)
	end := min(line+15, maxLine)
	return formatSourceRange(filePath, src, start, end)
}

// receiverTypeName extracts the type name from a method receiver.
func receiverTypeName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return ""
	}
	typ := fn.Recv.List[0].Type
	if star, ok := typ.(*ast.StarExpr); ok {
		typ = star.X
	}
	if ident, ok := typ.(*ast.Ident); ok {
		return ident.Name
	}
	return ""
}

// funcName returns a human-readable name for a function declaration.
func funcName(fn *ast.FuncDecl) string {
	recv := receiverTypeName(fn)
	if recv != "" {
		return recv + "." + fn.Name.Name
	}
	return fn.Name.Name
}

// readGoVersion reads the `go X.Y` directive from the go.mod in projectDir.
// Returns "" if not found.
func readGoVersion(projectDir string) string {
	data, err := os.ReadFile(filepath.Join(projectDir, "go.mod"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "go ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "go "))
		}
	}
	return ""
}

// parseGoVersion parses "1.22" or "1.22.0" into (major, minor).
func parseGoVersion(v string) (int, int) {
	parts := strings.Split(v, ".")
	if len(parts) < 2 {
		return 0, 0
	}
	major, _ := strconv.Atoi(parts[0])
	minor, _ := strconv.Atoi(parts[1])
	return major, minor
}

// goVersionAtLeast returns true if version string is >= major.minor.
func goVersionAtLeast(version string, major, minor int) bool {
	if version == "" {
		return false
	}
	maj, min := parseGoVersion(version)
	return maj > major || (maj == major && min >= minor)
}

// snippetLines extracts up to maxLines lines of source around a position.
func snippetLines(src []byte, fset *token.FileSet, pos token.Pos, context int) string {
	p := fset.Position(pos)
	lines := strings.Split(string(src), "\n")
	start := max(p.Line-context, 1)
	end := min(p.Line+context, len(lines))
	var parts []string
	for i := start; i <= end; i++ {
		parts = append(parts, fmt.Sprintf("%d: %s", i, lines[i-1]))
	}
	return strings.Join(parts, "\n")
}

// enclosingFuncName returns the name of the function containing the given line.
func enclosingFuncName(fset *token.FileSet, file *ast.File, line int) string {
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if fset.Position(fn.Pos()).Line <= line && fset.Position(fn.End()).Line >= line {
			return funcName(fn)
		}
	}
	return "<unknown>"
}

