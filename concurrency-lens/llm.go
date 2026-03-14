package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// callLLM invokes the Claude CLI with a prompt and returns the raw text response.
func callLLM(prompt string) (string, error) {
	cmd := exec.Command("claude", "-p", prompt)
	cmd.Env = filterEnv(os.Environ(), "CLAUDECODE")
	cmd.Stderr = os.Stderr
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("claude command failed: %w", err)
	}
	return string(output), nil
}

// callLLMForJSON calls the LLM and unmarshals the JSON response into target.
// On parse failure it retries once with a stricter prompt.
func callLLMForJSON(prompt string, target any) error {
	resp, err := callLLM(prompt)
	if err != nil {
		return err
	}
	jsonStr := extractJSON(resp)
	if err := json.Unmarshal([]byte(jsonStr), target); err != nil {
		// Retry: ask the LLM to fix its own output
		retryPrompt := fmt.Sprintf(
			"Your previous response was not valid JSON. Return ONLY the JSON, no prose, no explanation.\n\nOriginal task:\n%s\n\nYour broken response was:\n%s\n\nReturn valid JSON only.",
			prompt, truncateLLM(resp, 1000),
		)
		resp2, err2 := callLLM(retryPrompt)
		if err2 != nil {
			return fmt.Errorf("parsing LLM JSON response: %w\nraw response: %s", err, truncateLLM(resp, 500))
		}
		if err2 = json.Unmarshal([]byte(extractJSON(resp2)), target); err2 != nil {
			return fmt.Errorf("parsing LLM JSON response: %w\nraw response: %s", err, truncateLLM(resp, 500))
		}
	}
	return nil
}

// extractJSON extracts the first JSON object or array from an LLM response,
// stripping markdown fences and any leading prose.
func extractJSON(s string) string {
	s = strings.TrimSpace(s)
	// Strip ```json ... ``` or ``` ... ```
	for _, prefix := range []string{"```json", "```"} {
		if strings.HasPrefix(s, prefix) {
			s = strings.TrimPrefix(s, prefix)
			if idx := strings.LastIndex(s, "```"); idx >= 0 {
				s = s[:idx]
			}
			return strings.TrimSpace(s)
		}
	}
	// Find the first [ or { in case the LLM prefixed prose before the JSON
	start := -1
	for i, c := range s {
		if c == '[' || c == '{' {
			start = i
			break
		}
	}
	if start > 0 {
		s = s[start:]
	}
	return strings.TrimSpace(s)
}

func truncateLLM(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// filterEnv returns env with the named variable removed.
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
