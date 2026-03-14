package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/token"
	"os"
	"path/filepath"
	"time"
)

// GoroutineNode describes a spawned goroutine.
type GoroutineNode struct {
	Kind          string   `json:"kind"` // always "goroutine"
	ID            string   `json:"id"`
	File          string   `json:"file"`
	Line          int      `json:"line"`
	SpawnContext  string   `json:"spawn_context"` // enclosing function name
	Label         string   `json:"label"`         // best-effort name
	ChannelsRead  []string `json:"channels_read"`
	ChannelsWrite []string `json:"channels_write"`
	// LLM-populated
	Lifecycle     string `json:"lifecycle"`      // long_running | per_request | one_shot | unknown
	StopMechanism string `json:"stop_mechanism"` // context | done_channel | waitgroup | none | unknown
	LeakRisk      string `json:"leak_risk"`      // high | medium | low
	LeakReason    string `json:"leak_reason,omitempty"`
	LLMSummary    string `json:"llm_summary,omitempty"`
	SourceSnippet string `json:"source_snippet"`
}

// ChannelNode describes a channel and its usage sites.
type ChannelNode struct {
	Kind        string        `json:"kind"` // always "channel"
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	File        string        `json:"file"`
	Line        int           `json:"line"`
	ElementType string        `json:"element_type"`
	Buffered    bool          `json:"buffered"`
	BufferSize  string        `json:"buffer_size"` // string because it might be a const name
	Senders     []ChannelSite `json:"senders"`
	Receivers   []ChannelSite `json:"receivers"`
	Closers     []ChannelSite `json:"closers"`
}

// GraphEdge represents a communication relationship between goroutines via a channel.
type GraphEdge struct {
	Kind      string `json:"kind"` // always "edge"
	From      string `json:"from"` // goroutine ID or "main"
	To        string `json:"to"`
	Via       string `json:"via"` // channel name
	Direction string `json:"direction"` // sends | receives
	File      string `json:"file"`
	Line      int    `json:"line"`
}

// ChannelSite is a specific location where a channel is used.
type ChannelSite struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Context string `json:"context"` // enclosing function name
}

func runCheckGoroutines(args []string) error {
	fs := flag.NewFlagSet("check-goroutines", flag.ExitOnError)
	dir := fs.String("dir", "", "project directory to analyse")
	output := fs.String("o", "", "output JSON file (default: auto-named)")
	noLLM := fs.Bool("no-llm", false, "skip LLM lifecycle analysis")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dir == "" {
		return fmt.Errorf("-dir is required")
	}

	start := time.Now()
	items, err := checkGoroutines(*dir, !*noLLM)
	if err != nil {
		return err
	}

	raw := make([]json.RawMessage, 0, len(items))
	for _, it := range items {
		raw = append(raw, it)
	}

	result := CheckResult{
		Check:      "check-goroutines",
		ProjectDir: absPath(*dir),
		RunAt:      start,
		DurationMs: time.Since(start).Milliseconds(),
		ItemCount:  len(raw),
		Items:      raw,
	}

	outPath := *output
	if outPath == "" {
		outPath = OutputFileName(*dir, "check-goroutines", start)
	}
	if err := WriteCheckResult(result, outPath); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "check-goroutines: %d item(s) → %s\n", len(items), outPath)
	return nil
}

func checkGoroutines(projectDir string, useLLM bool) ([]json.RawMessage, error) {
	goroutines, channels, edges := discoverGoroutineGraph(projectDir)

	if useLLM {
		for i := range goroutines {
			g := &goroutines[i]
			if err := annotateGoroutineLifecycle(projectDir, g); err != nil {
				fmt.Fprintf(os.Stderr, "warning: LLM annotation failed for goroutine at %s:%d: %v\n",
					g.File, g.Line, err)
				g.Lifecycle = "unknown"
				g.StopMechanism = "unknown"
				g.LeakRisk = "unknown"
			}
		}
	}

	var items []json.RawMessage
	for _, g := range goroutines {
		items = append(items, MarshalItem(g))
	}
	for _, c := range channels {
		items = append(items, MarshalItem(c))
	}
	for _, e := range edges {
		items = append(items, MarshalItem(e))
	}
	return items, nil
}

func discoverGoroutineGraph(projectDir string) ([]GoroutineNode, []ChannelNode, []GraphEdge) {
	var goroutines []GoroutineNode
	var channels []ChannelNode
	var edges []GraphEdge

	goroutineCount := 0
	channelCount := 0

	// First pass: find all channels
	walkProject(projectDir, func(fset *token.FileSet, file *ast.File, src []byte, path string) {
		rel, _ := filepath.Rel(projectDir, path)
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			fnName := funcName(fn)
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				ident, ok := call.Fun.(*ast.Ident)
				if !ok || ident.Name != "make" || len(call.Args) < 1 {
					return true
				}
				chanType, ok := call.Args[0].(*ast.ChanType)
				if !ok {
					return true
				}
				channelCount++
				pos := fset.Position(call.Pos())
				buffered := len(call.Args) > 1
				bufSize := ""
				if buffered {
					bufSize = exprString(call.Args[1])
				}
				// Try to find the variable name this channel is assigned to
				chanName := fmt.Sprintf("ch_%d", channelCount)
				channels = append(channels, ChannelNode{
					Kind:        "channel",
					ID:          fmt.Sprintf("channel_%d", channelCount),
					Name:        chanName,
					File:        rel,
					Line:        pos.Line,
					ElementType: typeExprString(chanType.Value),
					Buffered:    buffered,
					BufferSize:  bufSize,
				})
				_ = fnName
				return true
			})
		}
	})

	// Second pass: find goroutine spawns and channel operations
	walkProject(projectDir, func(fset *token.FileSet, file *ast.File, src []byte, path string) {
		rel, _ := filepath.Rel(projectDir, path)
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			spawnCtx := funcName(fn)

			ast.Inspect(fn.Body, func(n ast.Node) bool {
				goStmt, ok := n.(*ast.GoStmt)
				if !ok {
					return true
				}
				goroutineCount++
				pos := fset.Position(goStmt.Pos())
				label := goroutineLabel(goStmt, spawnCtx, goroutineCount)
				id := fmt.Sprintf("goroutine_%d", goroutineCount)

				// Collect channel sends/receives in the goroutine body
				var reads, writes []string
				ast.Inspect(goStmt.Call, func(inner ast.Node) bool {
					switch e := inner.(type) {
					case *ast.SendStmt:
						name := exprString(e.Chan)
						writes = appendUnique(writes, name)
						edges = append(edges, GraphEdge{
							Kind:      "edge",
							From:      id,
							To:        "?",
							Via:       name,
							Direction: "sends",
							File:      rel,
							Line:      fset.Position(e.Pos()).Line,
						})
					case *ast.UnaryExpr:
						if e.Op.String() == "<-" {
							name := exprString(e.X)
							reads = appendUnique(reads, name)
							edges = append(edges, GraphEdge{
								Kind:      "edge",
								From:      "?",
								To:        id,
								Via:       name,
								Direction: "receives",
								File:      rel,
								Line:      fset.Position(e.Pos()).Line,
							})
						}
					}
					return true
				})

				snippet := enclosingFunction(fset, file, src, rel, pos.Line)
				if len(snippet) > 3000 {
					snippet = snippet[:3000] + "\n// ... (truncated)"
				}

				goroutines = append(goroutines, GoroutineNode{
					Kind:          "goroutine",
					ID:            id,
					File:          rel,
					Line:          pos.Line,
					SpawnContext:  spawnCtx,
					Label:         label,
					ChannelsRead:  reads,
					ChannelsWrite: writes,
					SourceSnippet: snippet,
				})
				return true
			})
		}
	})

	return goroutines, channels, edges
}

func goroutineLabel(goStmt *ast.GoStmt, spawnCtx string, n int) string {
	// If it's a named function call, use that name
	switch fun := goStmt.Call.Fun.(type) {
	case *ast.Ident:
		return fun.Name
	case *ast.SelectorExpr:
		return fun.Sel.Name
	case *ast.FuncLit:
		// Anonymous — use spawner name + ordinal
		return fmt.Sprintf("%s/anon#%d", spawnCtx, n)
	}
	return fmt.Sprintf("goroutine_%d", n)
}

func appendUnique(slice []string, s string) []string {
	for _, existing := range slice {
		if existing == s {
			return slice
		}
	}
	return append(slice, s)
}

func annotateGoroutineLifecycle(_ string, g *GoroutineNode) error {
	prompt := fmt.Sprintf(`You are analysing a Go goroutine for lifecycle safety.

Goroutine spawned at: %s:%d
Spawning function: %s
Label: %s

Source context:
%s

Questions:
1. Is this goroutine long-running, per-request, or one-shot?
2. What signal stops it? (context cancellation, done channel, WaitGroup, or nothing)
3. Can it outlive the object it references? Leak risk?
4. If it panics, is the panic recovered?

Respond with a JSON object only (no markdown):
{
  "lifecycle": "long_running|per_request|one_shot|unknown",
  "stop_mechanism": "context|done_channel|waitgroup|none|unknown",
  "leak_risk": "high|medium|low",
  "leak_reason": "<one sentence or empty>",
  "llm_summary": "<one sentence>"
}`, g.File, g.Line, g.SpawnContext, g.Label, g.SourceSnippet)

	type llmResult struct {
		Lifecycle     string `json:"lifecycle"`
		StopMechanism string `json:"stop_mechanism"`
		LeakRisk      string `json:"leak_risk"`
		LeakReason    string `json:"leak_reason"`
		LLMSummary    string `json:"llm_summary"`
	}
	var r llmResult
	if err := callLLMForJSON(prompt, &r); err != nil {
		return err
	}
	g.Lifecycle = r.Lifecycle
	g.StopMechanism = r.StopMechanism
	g.LeakRisk = r.LeakRisk
	g.LeakReason = r.LeakReason
	g.LLMSummary = r.LLMSummary
	return nil
}

