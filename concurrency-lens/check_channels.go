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

// ChannelLifecycleItem is a channel with its full lifecycle analysis.
type ChannelLifecycleItem struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	File        string        `json:"file"`
	Line        int           `json:"line"`
	ElementType string        `json:"element_type"`
	Buffered    bool          `json:"buffered"`
	BufferSize  string        `json:"buffer_size"`
	Senders     []ChannelSite `json:"senders"`
	Receivers   []ChannelSite `json:"receivers"`
	Closers     []ChannelSite `json:"closers"`
	// LLM-populated
	CloseOwner    string          `json:"close_owner"`
	Hazards       []ChannelHazard `json:"hazards"`
	LLMSummary    string          `json:"llm_summary,omitempty"`
	SourceContext string          `json:"source_context,omitempty"`
}

// ChannelHazard describes a potential problem with a channel.
type ChannelHazard struct {
	Kind     string `json:"kind"`     // send_after_close | unbuffered_deadlock | buffer_saturation | no_close | double_close
	Severity string `json:"severity"` // BUG | WARNING
	Detail   string `json:"detail"`
}

func runCheckChannels(args []string) error {
	fs := flag.NewFlagSet("check-channels", flag.ExitOnError)
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
	items, err := checkChannels(*dir, !*noLLM)
	if err != nil {
		return err
	}

	raw := make([]json.RawMessage, 0, len(items))
	for _, it := range items {
		raw = append(raw, MarshalItem(it))
	}

	result := CheckResult{
		Check:      "check-channels",
		ProjectDir: absPath(*dir),
		RunAt:      start,
		DurationMs: time.Since(start).Milliseconds(),
		ItemCount:  len(raw),
		Items:      raw,
	}

	outPath := *output
	if outPath == "" {
		outPath = OutputFileName(*dir, "check-channels", start)
	}
	if err := WriteCheckResult(result, outPath); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "check-channels: %d channel(s) → %s\n", len(items), outPath)
	return nil
}

func checkChannels(projectDir string, useLLM bool) ([]ChannelLifecycleItem, error) {
	items := discoverChannelLifecycles(projectDir)

	// Apply static hazard detection before LLM (always runs).
	addStaticHazards(items)
	extras := scanInFunctionSendAfterClose(projectDir, items)
	items = append(items, extras...)

	if useLLM {
		for i := range items {
			it := &items[i]
			if err := annotateChannelLifecycle(it); err != nil {
				fmt.Fprintf(os.Stderr, "warning: LLM annotation failed for channel %s: %v\n", it.Name, err)
			}
		}
	}

	return items, nil
}

// chanMakeRecord tracks a channel creation.
type chanMakeRecord struct {
	id          string
	name        string // variable name the channel is assigned to
	file        string
	line        int
	elemType    string
	buffered    bool
	bufSize     string
	sourceCtx   string // enclosing function source
}

func discoverChannelLifecycles(projectDir string) []ChannelLifecycleItem {
	// Pass 1: find all make(chan ...) calls in three contexts:
	//   a) assignment:        x := make(chan T)  /  s.ch = make(chan T)
	//   b) composite literal: &Foo{ch: make(chan T)}
	//   c) return literal:    return &Foo{ch: make(chan T)}  (covered by b)
	var makes []chanMakeRecord
	count := 0

	walkProject(projectDir, func(fset *token.FileSet, file *ast.File, src []byte, path string) {
		rel, _ := filepath.Rel(projectDir, path)
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			fnName := funcName(fn)

			ast.Inspect(fn.Body, func(n ast.Node) bool {
				switch node := n.(type) {
				case *ast.AssignStmt:
					// x := make(chan T)  or  s.field = make(chan T)
					for i, rhs := range node.Rhs {
						call, ok := rhs.(*ast.CallExpr)
						if !ok {
							continue
						}
						rec, ok := extractChanMake(fset, file, src, rel, fnName, call, &count)
						if !ok {
							continue
						}
						if i < len(node.Lhs) {
							rec.name = exprString(node.Lhs[i])
						}
						makes = append(makes, rec)
					}

				case *ast.CompositeLit:
					// &Foo{fieldName: make(chan T)}
					for _, elt := range node.Elts {
						kv, ok := elt.(*ast.KeyValueExpr)
						if !ok {
							continue
						}
						call, ok := kv.Value.(*ast.CallExpr)
						if !ok {
							continue
						}
						rec, ok := extractChanMake(fset, file, src, rel, fnName, call, &count)
						if !ok {
							continue
						}
						rec.name = exprString(kv.Key)
						makes = append(makes, rec)
					}
				}
				return true
			})
		}
	})

	// Pass 2: find send/receive/close operations for each channel.
	items := make([]ChannelLifecycleItem, 0, len(makes))
	for _, m := range makes {
		items = append(items, ChannelLifecycleItem{
			ID:            m.id,
			Name:          m.name,
			File:          m.file,
			Line:          m.line,
			ElementType:   m.elemType,
			Buffered:      m.buffered,
			BufferSize:    m.bufSize,
			SourceContext: m.sourceCtx,
		})
	}

	walkProject(projectDir, func(fset *token.FileSet, file *ast.File, src []byte, path string) {
		rel, _ := filepath.Rel(projectDir, path)
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			fnName := funcName(fn)

			ast.Inspect(fn.Body, func(n ast.Node) bool {
				switch e := n.(type) {
				case *ast.SendStmt:
					name := exprString(e.Chan)
					site := ChannelSite{File: rel, Line: lineOf(fset, e.Pos()), Context: fnName}
					for i := range items {
						if chanNameMatch(items[i].Name, name) {
							items[i].Senders = append(items[i].Senders, site)
						}
					}
				case *ast.UnaryExpr:
					if e.Op.String() == "<-" {
						name := exprString(e.X)
						site := ChannelSite{File: rel, Line: lineOf(fset, e.Pos()), Context: fnName}
						for i := range items {
							if chanNameMatch(items[i].Name, name) {
								items[i].Receivers = append(items[i].Receivers, site)
							}
						}
					}
				case *ast.RangeStmt:
					// for v := range ch { ... }
					name := exprString(e.X)
					site := ChannelSite{File: rel, Line: lineOf(fset, e.Pos()), Context: fnName}
					for i := range items {
						if chanNameMatch(items[i].Name, name) {
							items[i].Receivers = append(items[i].Receivers, site)
						}
					}
				case *ast.ExprStmt:
					call, ok := e.X.(*ast.CallExpr)
					if !ok {
						return true
					}
					ident, ok := call.Fun.(*ast.Ident)
					if !ok || ident.Name != "close" || len(call.Args) != 1 {
						return true
					}
					name := exprString(call.Args[0])
					site := ChannelSite{File: rel, Line: lineOf(fset, e.Pos()), Context: fnName}
					for i := range items {
						if chanNameMatch(items[i].Name, name) {
							items[i].Closers = append(items[i].Closers, site)
						}
					}
				}
				return true
			})
		}
	})

	return items
}

// extractChanMake checks whether call is make(chan T[, size]) and returns a chanMakeRecord.
func extractChanMake(fset *token.FileSet, file *ast.File, src []byte, rel, fnName string, call *ast.CallExpr, count *int) (chanMakeRecord, bool) {
	ident, ok := call.Fun.(*ast.Ident)
	if !ok || ident.Name != "make" || len(call.Args) < 1 {
		return chanMakeRecord{}, false
	}
	chanType, ok := call.Args[0].(*ast.ChanType)
	if !ok {
		return chanMakeRecord{}, false
	}
	*count++
	buffered := len(call.Args) > 1
	bufSize := ""
	if buffered {
		bufSize = exprString(call.Args[1])
	}
	pos := fset.Position(call.Pos())
	ctx := enclosingFunction(fset, file, src, rel, pos.Line)
	if len(ctx) > 2000 {
		ctx = ctx[:2000] + "\n// ..."
	}
	return chanMakeRecord{
		id:        fmt.Sprintf("chan_%d", *count),
		name:      fmt.Sprintf("ch_%d", *count), // caller overwrites with real name
		file:      rel,
		line:      pos.Line,
		elemType:  typeExprString(chanType.Value),
		buffered:  buffered,
		bufSize:   bufSize,
		sourceCtx: fmt.Sprintf("// function: %s\n%s", fnName, ctx),
	}, true
}

// scanInFunctionSendAfterClose finds close(ch) followed by ch<-x within a single
// function body, even for channels we never saw created (e.g. parameters).
// Returns any new ChannelLifecycleItems for parameter/untracked channels.
func scanInFunctionSendAfterClose(projectDir string, existing []ChannelLifecycleItem) []ChannelLifecycleItem {
	existingNames := make(map[string]bool)
	for _, it := range existing {
		existingNames[it.Name] = true
	}

	var extras []ChannelLifecycleItem
	count := len(existing)

	walkProject(projectDir, func(fset *token.FileSet, file *ast.File, _ []byte, path string) {
		rel, _ := filepath.Rel(projectDir, path)
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			fnName := funcName(fn)

			// Collect close() calls in statement order
			var closeSites []struct {
				name string
				line int
			}
			// Collect sends
			var sendSites []struct {
				name string
				line int
			}

			ast.Inspect(fn.Body, func(n ast.Node) bool {
				switch e := n.(type) {
				case *ast.ExprStmt:
					call, ok := e.X.(*ast.CallExpr)
					if !ok {
						return true
					}
					ident, ok := call.Fun.(*ast.Ident)
					if !ok || ident.Name != "close" || len(call.Args) != 1 {
						return true
					}
					closeSites = append(closeSites, struct {
						name string
						line int
					}{exprString(call.Args[0]), lineOf(fset, e.Pos())})
				case *ast.SendStmt:
					sendSites = append(sendSites, struct {
						name string
						line int
					}{exprString(e.Chan), lineOf(fset, e.Pos())})
				}
				return true
			})

			for _, cl := range closeSites {
				for _, snd := range sendSites {
					if snd.line <= cl.line {
						continue
					}
					shortCl := shortFieldName(cl.name)
					shortSnd := shortFieldName(snd.name)
					if cl.name != snd.name && shortCl != shortSnd {
						continue
					}
					// Found send after close; add to existing item or create a new one
					found := false
					for i := range existing {
						if chanNameMatch(existing[i].Name, cl.name) {
							existing[i].Hazards = append(existing[i].Hazards, ChannelHazard{
								Kind:     "send_after_close",
								Severity: "BUG",
								Detail:   fmt.Sprintf("close at line %d precedes send at line %d in %s", cl.line, snd.line, fnName),
							})
							found = true
						}
					}
					if !found && !existingNames[cl.name] {
						count++
						extras = append(extras, ChannelLifecycleItem{
							ID:   fmt.Sprintf("chan_%d", count),
							Name: cl.name,
							File: rel,
							Line: cl.line,
							Closers: []ChannelSite{{File: rel, Line: cl.line, Context: fnName}},
							Senders: []ChannelSite{{File: rel, Line: snd.line, Context: fnName}},
							Hazards: []ChannelHazard{{
								Kind:     "send_after_close",
								Severity: "BUG",
								Detail:   fmt.Sprintf("close at line %d precedes send at line %d in %s", cl.line, snd.line, fnName),
							}},
						})
						existingNames[cl.name] = true
					}
				}
			}
		}
	})

	return extras
}

// addStaticHazards does a per-function scan for hazards detectable without LLM:
//   - send_after_close: close(ch) followed by ch <- x in the same function
//   - double_close:     close(ch) appears at 2+ sites for the same channel
func addStaticHazards(items []ChannelLifecycleItem) {
	for i := range items {
		it := &items[i]

		// double_close: more than one closer site
		if len(it.Closers) > 1 {
			it.Hazards = append(it.Hazards, ChannelHazard{
				Kind:     "double_close",
				Severity: "BUG",
				Detail:   fmt.Sprintf("channel closed at %d sites — panics if both execute", len(it.Closers)),
			})
		}

		// send_after_close: any sender appears after a closer (by line) in the same file/func
		for _, closer := range it.Closers {
			for _, sender := range it.Senders {
				if sender.File == closer.File && sender.Context == closer.Context && sender.Line > closer.Line {
					it.Hazards = append(it.Hazards, ChannelHazard{
						Kind:     "send_after_close",
						Severity: "BUG",
						Detail: fmt.Sprintf("close at line %d precedes send at line %d in %s",
							closer.Line, sender.Line, closer.Context),
					})
				}
			}
		}

		// no_close: unbuffered channel with senders but no closer
		if !it.Buffered && len(it.Senders) > 0 && len(it.Closers) == 0 {
			it.Hazards = append(it.Hazards, ChannelHazard{
				Kind:     "no_close",
				Severity: "WARNING",
				Detail:   "channel is never closed — receivers will block forever if all senders stop",
			})
		}
	}
}

// chanNameMatch returns true if the channel reference matches the channel's tracked name.
// Handles direct names ("done") and selector expressions ("s.done").
func chanNameMatch(trackName, refName string) bool {
	if trackName == refName {
		return true
	}
	// short name match: "s.done" matches "done"
	shortTrack := shortFieldName(trackName)
	shortRef := shortFieldName(refName)
	return shortTrack != "" && shortTrack == shortRef
}

func annotateChannelLifecycle(ch *ChannelLifecycleItem) error {
	sendList := formatSites(ch.Senders)
	recvList := formatSites(ch.Receivers)
	closeInfo := "none found"
	if len(ch.Closers) > 0 {
		closeInfo = formatSites(ch.Closers)
	}

	prompt := fmt.Sprintf(`You are auditing a Go channel for lifecycle correctness.

Channel: %s
Type: chan %s
Buffered: %v (size: %s)
Created at: %s:%d

Senders (%d): %s
Receivers (%d): %s
Close sites: %s

Source context:
%s

Analyse:
1. Can a sender send after close? (panic risk)
2. Can this deadlock (sender and receiver in same goroutine)?
3. For buffered: can the buffer fill and block the sender indefinitely?
4. Is it closed exactly once? Who should own the close?

Respond with JSON only (no markdown):
{
  "close_owner": "<description of who should close, or 'none needed'>",
  "hazards": [
    { "kind": "send_after_close|unbuffered_deadlock|buffer_saturation|no_close|double_close", "severity": "BUG|WARNING", "detail": "..." }
  ],
  "llm_summary": "<one sentence>"
}`,
		ch.Name, ch.ElementType, ch.Buffered, ch.BufferSize, ch.File, ch.Line,
		len(ch.Senders), sendList,
		len(ch.Receivers), recvList,
		closeInfo,
		ch.SourceContext)

	type llmResult struct {
		CloseOwner string          `json:"close_owner"`
		Hazards    []ChannelHazard `json:"hazards"`
		LLMSummary string          `json:"llm_summary"`
	}
	var r llmResult
	if err := callLLMForJSON(prompt, &r); err != nil {
		return err
	}
	ch.CloseOwner = r.CloseOwner
	ch.Hazards = r.Hazards
	ch.LLMSummary = r.LLMSummary
	return nil
}

func formatSites(sites []ChannelSite) string {
	if len(sites) == 0 {
		return "none"
	}
	var parts []string
	for _, s := range sites {
		parts = append(parts, fmt.Sprintf("%s:%d (%s)", s.File, s.Line, s.Context))
	}
	return strings.Join(parts, ", ")
}
