package main

import (
	"fmt"
	"os"
)

const usage = `codereview-buddy — targeted concurrency review for Go projects

Usage:
  codereview-buddy triage   -semgrep <json-file> -dir <project-dir> [-o prompts.json]
  codereview-buddy evaluate -prompts <prompts.json> [-o findings.json]
  codereview-buddy report   -findings <findings.json> [-format text|markdown]

Subcommands:
  triage     Parse semgrep JSON + grep-based scans, extract function context
             via go/ast, and build per-finding LLM prompts.
  evaluate   Batch micro-prompts to Claude CLI, parse structured responses
             into findings.
  report     Aggregate findings from all sources and output a report.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(1)
	}

	var err error
	switch os.Args[1] {
	case "triage":
		err = runTriage(os.Args[2:])
	case "evaluate":
		err = runEvaluate(os.Args[2:])
	case "report":
		err = runReport(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n\n%s", os.Args[1], usage)
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
