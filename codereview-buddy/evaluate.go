package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// Finding represents a single code review finding from the LLM.
type Finding struct {
	File     string `json:"file"`
	Line     int    `json:"line"`
	RuleID   string `json:"rule_id"`
	Severity string `json:"severity"`
	Summary  string `json:"summary"`
}

func runEvaluate(args []string) error {
	fs := flag.NewFlagSet("evaluate", flag.ExitOnError)
	promptsFile := fs.String("prompts", "", "path to prompts JSON from triage step")
	outputFile := fs.String("o", "", "output file for findings JSON (default: stdout)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *promptsFile == "" {
		return fmt.Errorf("-prompts flag is required")
	}

	// Read prompts
	data, err := os.ReadFile(*promptsFile)
	if err != nil {
		return fmt.Errorf("reading prompts: %w", err)
	}

	var prompts []TriagePrompt
	if err := json.Unmarshal(data, &prompts); err != nil {
		return fmt.Errorf("parsing prompts: %w", err)
	}

	if len(prompts) == 0 {
		fmt.Fprintln(os.Stderr, "no prompts to evaluate")
		return outputFindings(nil, *outputFile)
	}

	// Build a single batched prompt for Claude
	batchedPrompt := buildBatchedPrompt(prompts)

	// Call Claude CLI
	fmt.Fprintf(os.Stderr, "evaluating %d findings with Claude...\n", len(prompts))
	response, err := callClaude(batchedPrompt)
	if err != nil {
		return fmt.Errorf("calling Claude: %w", err)
	}

	// Parse response into findings
	findings := parseFindings(response, prompts)

	fmt.Fprintf(os.Stderr, "found %d issues (%d prompts evaluated)\n", len(findings), len(prompts))

	return outputFindings(findings, *outputFile)
}

// buildBatchedPrompt combines all micro-prompts into a single delimited prompt.
func buildBatchedPrompt(prompts []TriagePrompt) string {
	var b strings.Builder
	b.WriteString(`You are a Go concurrency expert performing a targeted code review.
Below are multiple review sections, each delimited by "=== SECTION N ===".
For each section, analyze the code and respond with your findings.

RESPONSE FORMAT:
For each section, start with "=== RESPONSE N ===" and then either:
- "NO_ISSUES_FOUND" if the code is safe
- One or more findings in this exact format:
  FILE: <file path>
  LINE: <line number>
  SEVERITY: BUG or WARNING
  SUMMARY: <one-line description>

Do NOT report style issues, only actual concurrency bugs or race conditions.

`)

	for i, p := range prompts {
		fmt.Fprintf(&b, "=== SECTION %d ===\n", i+1)
		fmt.Fprintf(&b, "Rule: %s | File: %s | Line: %d\n\n", p.RuleID, p.File, p.Line)
		b.WriteString(p.Prompt)
		b.WriteString("\n\n")
	}

	return b.String()
}

// callClaude invokes the Claude CLI with the given prompt and returns the response.
func callClaude(prompt string) (string, error) {
	cmd := exec.Command("claude", "-p", prompt)
	// Clear CLAUDECODE env var to allow nested invocation
	cmd.Env = filterEnv(os.Environ(), "CLAUDECODE")
	cmd.Stderr = os.Stderr
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("claude command failed: %w", err)
	}
	return string(output), nil
}

var (
	findingFileRe     = regexp.MustCompile(`(?i)FILE:\s*(.+)`)
	findingLineRe     = regexp.MustCompile(`(?i)LINE:\s*(\d+)`)
	findingSeverityRe = regexp.MustCompile(`(?i)SEVERITY:\s*(BUG|WARNING)`)
	findingSummaryRe  = regexp.MustCompile(`(?i)SUMMARY:\s*(.+)`)
	sectionHeaderRe   = regexp.MustCompile(`===\s*RESPONSE\s+(\d+)\s*===`)
)

// parseFindings extracts structured findings from the Claude response.
func parseFindings(response string, prompts []TriagePrompt) []Finding {
	var findings []Finding

	// Split by response sections
	sections := sectionHeaderRe.Split(response, -1)
	sectionIndices := sectionHeaderRe.FindAllStringSubmatch(response, -1)

	// Process each section
	for i, idxMatch := range sectionIndices {
		sectionNum, err := strconv.Atoi(idxMatch[1])
		if err != nil || sectionNum < 1 || sectionNum > len(prompts) {
			continue
		}

		sectionText := ""
		if i+1 < len(sections) {
			sectionText = sections[i+1]
		}

		if strings.Contains(sectionText, "NO_ISSUES_FOUND") {
			continue
		}

		// Parse findings from this section
		sectionFindings := parseSectionFindings(sectionText, prompts[sectionNum-1])
		findings = append(findings, sectionFindings...)
	}

	// If no section headers found, try parsing the whole response
	if len(sectionIndices) == 0 && len(prompts) == 1 {
		if !strings.Contains(response, "NO_ISSUES_FOUND") {
			findings = append(findings, parseSectionFindings(response, prompts[0])...)
		}
	}

	return findings
}

// parseSectionFindings extracts findings from a single response section.
func parseSectionFindings(text string, prompt TriagePrompt) []Finding {
	var findings []Finding

	scanner := bufio.NewScanner(strings.NewReader(text))
	var current Finding
	current.RuleID = prompt.RuleID
	hasFile := false

	for scanner.Scan() {
		line := scanner.Text()

		if m := findingFileRe.FindStringSubmatch(line); m != nil {
			// If we had a previous finding, save it
			if hasFile && current.Summary != "" {
				findings = append(findings, current)
			}
			current = Finding{RuleID: prompt.RuleID}
			current.File = strings.TrimSpace(m[1])
			hasFile = true
		} else if m := findingLineRe.FindStringSubmatch(line); m != nil {
			current.Line, _ = strconv.Atoi(m[1])
		} else if m := findingSeverityRe.FindStringSubmatch(line); m != nil {
			current.Severity = strings.ToUpper(strings.TrimSpace(m[1]))
		} else if m := findingSummaryRe.FindStringSubmatch(line); m != nil {
			current.Summary = strings.TrimSpace(m[1])
		}
	}

	// Don't forget the last finding
	if hasFile && current.Summary != "" {
		findings = append(findings, current)
	}

	// If structured parsing found nothing but there's text, create a generic finding
	if len(findings) == 0 && len(strings.TrimSpace(text)) > 0 && !strings.Contains(text, "NO_ISSUES_FOUND") {
		// Check if there's meaningful content (not just whitespace/formatting)
		cleaned := strings.TrimSpace(text)
		if len(cleaned) > 20 {
			findings = append(findings, Finding{
				File:     prompt.File,
				Line:     prompt.Line,
				RuleID:   prompt.RuleID,
				Severity: "WARNING",
				Summary:  truncate(cleaned, 200),
			})
		}
	}

	return findings
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func outputFindings(findings []Finding, outputFile string) error {
	out := os.Stdout
	if outputFile != "" {
		f, err := os.Create(outputFile)
		if err != nil {
			return err
		}
		defer f.Close()
		out = f
	}

	if findings == nil {
		findings = []Finding{}
	}

	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(findings)
}

// filterEnv returns a copy of env with the named variable removed.
func filterEnv(env []string, name string) []string {
	prefix := name + "="
	var filtered []string
	for _, e := range env {
		if !strings.HasPrefix(e, prefix) {
			filtered = append(filtered, e)
		}
	}
	return filtered
}
