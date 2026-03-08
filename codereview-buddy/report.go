package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
)

func runReport(args []string) error {
	fs := flag.NewFlagSet("report", flag.ExitOnError)
	findingsFile := fs.String("findings", "", "path to findings JSON from evaluate step")
	format := fs.String("format", "text", "output format: text or markdown")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *findingsFile == "" {
		return fmt.Errorf("-findings flag is required")
	}

	data, err := os.ReadFile(*findingsFile)
	if err != nil {
		return fmt.Errorf("reading findings: %w", err)
	}

	var findings []Finding
	if err := json.Unmarshal(data, &findings); err != nil {
		return fmt.Errorf("parsing findings: %w", err)
	}

	// Deduplicate
	findings = dedup(findings)

	// Sort by severity (BUG first), then file, then line
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Severity != findings[j].Severity {
			return severityOrder(findings[i].Severity) < severityOrder(findings[j].Severity)
		}
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		return findings[i].Line < findings[j].Line
	})

	switch *format {
	case "markdown":
		printMarkdown(findings)
	default:
		printText(findings)
	}

	// Exit non-zero if any BUG or WARNING findings
	for _, f := range findings {
		if f.Severity == "BUG" || f.Severity == "WARNING" {
			os.Exit(1)
		}
	}

	return nil
}

func severityOrder(s string) int {
	switch s {
	case "ERROR":
		return 0
	case "BUG":
		return 1
	case "WARNING":
		return 2
	case "INFO":
		return 3
	default:
		return 4
	}
}

func dedup(findings []Finding) []Finding {
	seen := make(map[string]bool)
	var result []Finding
	for _, f := range findings {
		key := fmt.Sprintf("%s:%d:%s", f.File, f.Line, f.RuleID)
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, f)
	}
	return result
}

func printText(findings []Finding) {
	if len(findings) == 0 {
		fmt.Println("No issues found.")
		return
	}

	fmt.Printf("Found %d issue(s):\n\n", len(findings))
	for _, f := range findings {
		fmt.Printf("  [%s] %s:%d\n", f.Severity, f.File, f.Line)
		fmt.Printf("    Rule: %s\n", f.RuleID)
		fmt.Printf("    %s\n\n", f.Summary)
	}
}

func printMarkdown(findings []Finding) {
	if len(findings) == 0 {
		fmt.Println("## Code Review: No Issues Found")
		fmt.Println()
		fmt.Println("All concurrency patterns reviewed — no bugs detected.")
		return
	}

	fmt.Println("## Code Review Findings")
	fmt.Println()

	// Group by severity
	groups := map[string][]Finding{}
	for _, f := range findings {
		groups[f.Severity] = append(groups[f.Severity], f)
	}

	for _, sev := range []string{"ERROR", "BUG", "WARNING", "INFO"} {
		group := groups[sev]
		if len(group) == 0 {
			continue
		}

		icon := severityIcon(sev)
		fmt.Printf("### %s %s (%d)\n\n", icon, sev, len(group))

		for _, f := range group {
			fmt.Printf("- **%s:%d** — %s\n", f.File, f.Line, f.Summary)
			fmt.Printf("  - Rule: `%s`\n", f.RuleID)
		}
		fmt.Println()
	}

	// Summary
	var parts []string
	for _, sev := range []string{"ERROR", "BUG", "WARNING", "INFO"} {
		if n := len(groups[sev]); n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, strings.ToLower(sev)))
		}
	}
	fmt.Printf("**Total: %s**\n", strings.Join(parts, ", "))
}

func severityIcon(s string) string {
	switch s {
	case "ERROR", "BUG":
		return "🔴"
	case "WARNING":
		return "🟡"
	default:
		return "🔵"
	}
}
