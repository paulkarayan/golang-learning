package main

import (
	"fmt"
	"os"
)

const usage = `concurrency-lens — exhaustive concurrency analysis for Go projects

Usage:
  concurrency-lens <check> -dir <project-dir> [-o output.json] [-no-llm]
  concurrency-lens serve [-port 8765] [-results-dir .]

Checks (AST only — fast, no LLM):
  check-closure       goroutines closing over loop variables
  check-maps          map writes inside goroutines without a lock
  check-wg            WaitGroup.Add() called inside goroutine body
  check-defer-unlock  defer mu.Unlock() inside a for loop

Checks (AST + LLM — requires ANTHROPIC_API_KEY via claude CLI):
  check-ownership     catalog every variable: owner, protection, green/yellow/red
  check-goroutines    goroutine graph: lifecycle and leak risk per goroutine
  check-channels      channel lifecycle: send-after-close, deadlock, buffer issues
  check-locks         lock ordering: conflicting acquisition sequences

Flags:
  -dir         project directory to analyse (required for check commands)
  -o           output JSON file (default: <project>_<check>_<timestamp>.json)
  -no-llm      skip LLM annotation, run AST discovery phase only

Web UI:
  serve        start the web UI (opens browser automatically)
  -port        HTTP port (default 8765)
  -results-dir directory for output JSON files (default: current dir)
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(1)
	}

	var err error
	switch os.Args[1] {
	case "check-closure":
		err = runCheckClosure(os.Args[2:])
	case "check-maps":
		err = runCheckMaps(os.Args[2:])
	case "check-wg":
		err = runCheckWG(os.Args[2:])
	case "check-defer-unlock":
		err = runCheckDefer(os.Args[2:])
	case "check-ownership":
		err = runCheckOwnership(os.Args[2:])
	case "check-goroutines":
		err = runCheckGoroutines(os.Args[2:])
	case "check-channels":
		err = runCheckChannels(os.Args[2:])
	case "check-locks":
		err = runCheckLocks(os.Args[2:])
	case "serve":
		err = runServe(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %q\n\n%s", os.Args[1], usage)
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
