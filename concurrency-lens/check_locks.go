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

// LockSequence is the ordered sequence of locks acquired in one function.
type LockSequence struct {
	FuncName string        `json:"func_name"`
	File     string        `json:"file"`
	Line     int           `json:"line"`
	Locks    []LockAcquire `json:"locks"`
}

// LockAcquire is a single Lock() or RLock() call.
type LockAcquire struct {
	Name string `json:"name"` // e.g. "mu", "s.mu", "b.accountA.mu"
	Kind string `json:"kind"` // Lock | RLock
	Line int    `json:"line"`
}

// LockOrderFinding reports a pair of functions that acquire overlapping locks in different orders.
type LockOrderFinding struct {
	FuncA        string        `json:"func_a"`
	FileA        string        `json:"file_a"`
	LineA        int           `json:"line_a"`
	LocksA       []LockAcquire `json:"locks_a"`
	FuncB        string        `json:"func_b"`
	FileB        string        `json:"file_b"`
	LineB        int           `json:"line_b"`
	LocksB       []LockAcquire `json:"locks_b"`
	SharedLocks  []string      `json:"shared_locks"`
	Hazard       string        `json:"hazard"`   // deadlock | potential_deadlock | none
	Severity     string        `json:"severity"` // BUG | WARNING | INFO
	LLMDetail    string        `json:"llm_detail,omitempty"`
	SourceA      string        `json:"source_a,omitempty"`
	SourceB      string        `json:"source_b,omitempty"`
}

func runCheckLocks(args []string) error {
	fs := flag.NewFlagSet("check-locks", flag.ExitOnError)
	dir := fs.String("dir", "", "project directory to analyse")
	output := fs.String("o", "", "output JSON file (default: auto-named)")
	noLLM := fs.Bool("no-llm", false, "skip LLM analysis")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dir == "" {
		return fmt.Errorf("-dir is required")
	}

	start := time.Now()
	findings, err := checkLocks(*dir, !*noLLM)
	if err != nil {
		return err
	}

	raw := make([]json.RawMessage, 0, len(findings))
	for _, f := range findings {
		raw = append(raw, MarshalItem(f))
	}

	result := CheckResult{
		Check:      "check-locks",
		ProjectDir: absPath(*dir),
		RunAt:      start,
		DurationMs: time.Since(start).Milliseconds(),
		ItemCount:  len(raw),
		Items:      raw,
	}

	outPath := *output
	if outPath == "" {
		outPath = OutputFileName(*dir, "check-locks", start)
	}
	if err := WriteCheckResult(result, outPath); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "check-locks: %d finding(s) → %s\n", len(findings), outPath)
	return nil
}

func checkLocks(projectDir string, useLLM bool) ([]LockOrderFinding, error) {
	sequences := collectLockSequences(projectDir)

	// Find pairs with conflicting lock orders
	conflicts := findConflictingPairs(sequences)

	if useLLM {
		for i := range conflicts {
			c := &conflicts[i]
			if err := annotateLockConflict(c); err != nil {
				fmt.Fprintf(os.Stderr, "warning: LLM annotation failed for lock conflict %s/%s: %v\n",
					c.FuncA, c.FuncB, err)
				c.Hazard = "potential_deadlock"
				c.Severity = "WARNING"
				c.LLMDetail = "LLM annotation failed — review manually"
			}
		}
	} else {
		for i := range conflicts {
			conflicts[i].Hazard = "potential_deadlock"
			conflicts[i].Severity = "WARNING"
		}
	}

	return conflicts, nil
}

func collectLockSequences(projectDir string) []LockSequence {
	var sequences []LockSequence

	walkProject(projectDir, func(fset *token.FileSet, file *ast.File, src []byte, path string) {
		rel, _ := filepath.Rel(projectDir, path)

		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}

			var locks []LockAcquire
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				kind := ""
				switch sel.Sel.Name {
				case "Lock":
					kind = "Lock"
				case "RLock":
					kind = "RLock"
				}
				if kind == "" {
					return true
				}
				lockName := exprString(sel.X)
				pos := fset.Position(call.Pos())
				locks = append(locks, LockAcquire{
					Name: lockName,
					Kind: kind,
					Line: pos.Line,
				})
				return true
			})

			if len(locks) >= 2 {
				pos := fset.Position(fn.Pos())
				src2 := enclosingFunction(fset, file, src, rel, pos.Line)
				sequences = append(sequences, LockSequence{
					FuncName: funcName(fn),
					File:     rel,
					Line:     pos.Line,
					Locks:    locks,
				})
				// Attach source to the sequence for later use
				_ = src2
			}
		}
	})

	return sequences
}

// findConflictingPairs finds pairs of functions that acquire the same set of mutexes
// in different orders.
func findConflictingPairs(sequences []LockSequence) []LockOrderFinding {
	var findings []LockOrderFinding

	for i := 0; i < len(sequences); i++ {
		for j := i + 1; j < len(sequences); j++ {
			a, b := sequences[i], sequences[j]
			shared := sharedLockNames(a.Locks, b.Locks)
			if len(shared) < 2 {
				continue
			}
			// Check if the order of shared locks is consistent
			orderA := lockOrder(a.Locks, shared)
			orderB := lockOrder(b.Locks, shared)
			if isConflictingOrder(orderA, orderB) {
				findings = append(findings, LockOrderFinding{
					FuncA:       a.FuncName,
					FileA:       a.File,
					LineA:       a.Line,
					LocksA:      a.Locks,
					FuncB:       b.FuncName,
					FileB:       b.File,
					LineB:       b.Line,
					LocksB:      b.Locks,
					SharedLocks: shared,
				})
			}
		}
	}

	return findings
}

// sharedLockNames returns the base names (normalised) shared between two lock sequences.
func sharedLockNames(a, b []LockAcquire) []string {
	setA := make(map[string]bool)
	for _, l := range a {
		setA[normaliseLockName(l.Name)] = true
	}
	var shared []string
	seen := make(map[string]bool)
	for _, l := range b {
		n := normaliseLockName(l.Name)
		if setA[n] && !seen[n] {
			shared = append(shared, n)
			seen[n] = true
		}
	}
	return shared
}

// normaliseLockName strips field accessor prefixes to compare across receivers.
// e.g. "s.mu" → "mu", "b.accountA.mu" → "accountA.mu"
func normaliseLockName(name string) string {
	parts := strings.SplitN(name, ".", 2)
	if len(parts) == 2 {
		// if first part looks like a single-char receiver (s, b, m), strip it
		if len(parts[0]) <= 2 {
			return parts[1]
		}
	}
	return name
}

// lockOrder returns the position of each shared lock in the given sequence.
func lockOrder(locks []LockAcquire, shared []string) map[string]int {
	result := make(map[string]int)
	pos := 0
	for _, l := range locks {
		n := normaliseLockName(l.Name)
		for _, s := range shared {
			if n == s {
				result[s] = pos
				pos++
			}
		}
	}
	return result
}

// isConflictingOrder returns true if the relative order of shared locks differs.
func isConflictingOrder(orderA, orderB map[string]int) bool {
	// Build pairs and check if any pair has inverted relative order
	names := make([]string, 0, len(orderA))
	for k := range orderA {
		names = append(names, k)
	}
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			ai, bi := orderA[names[i]], orderA[names[j]]
			ai2, bi2 := orderB[names[i]], orderB[names[j]]
			// In A: names[i] before names[j]? Compare in B.
			aOrder := ai < bi   // true if i < j in A
			bOrder := ai2 < bi2 // true if i < j in B
			if aOrder != bOrder {
				return true
			}
		}
	}
	return false
}

func annotateLockConflict(f *LockOrderFinding) error {
	prompt := fmt.Sprintf(`You are checking for lock-ordering deadlocks in Go.

Two functions acquire the same mutexes in different orders:

Function A: %s (%s:%d)
Lock acquisition order: %s

Function B: %s (%s:%d)
Lock acquisition order: %s

Shared locks: %s

Questions:
1. Can both functions run concurrently in different goroutines?
2. If yes, is the inverted lock order a real deadlock risk?
3. Are there guards preventing concurrent execution?

Respond with JSON only (no markdown):
{
  "hazard": "deadlock|potential_deadlock|none",
  "severity": "BUG|WARNING|INFO",
  "detail": "<one to two sentence explanation>",
  "can_run_concurrently": true
}`,
		f.FuncA, f.FileA, f.LineA, formatLockOrder(f.LocksA),
		f.FuncB, f.FileB, f.LineB, formatLockOrder(f.LocksB),
		strings.Join(f.SharedLocks, ", "))

	type llmResult struct {
		Hazard              string `json:"hazard"`
		Severity            string `json:"severity"`
		Detail              string `json:"detail"`
		CanRunConcurrently  bool   `json:"can_run_concurrently"`
	}
	var r llmResult
	if err := callLLMForJSON(prompt, &r); err != nil {
		return err
	}
	f.Hazard = r.Hazard
	f.Severity = r.Severity
	f.LLMDetail = r.Detail
	return nil
}

func formatLockOrder(locks []LockAcquire) string {
	var parts []string
	for _, l := range locks {
		parts = append(parts, fmt.Sprintf("%s.%s()", l.Name, l.Kind))
	}
	return strings.Join(parts, " → ")
}
