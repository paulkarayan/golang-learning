---
agent_num: 1
provider: claude
---

Let me look at how agents are spawned and how user content flows into commands.The agents use `bash -lc` with constructed command strings. This is critical. Let me examine how prompts flow into those commands.Now let me find the `shellQuote` function - this is critical since Claude passes prompts via command line argument through bash.Let me check the gist creation in run.go and also look for other areas where attacker-controlled content reaches shell commands.Let me check how agents are created and where model parameters come from.Let me check if there's any config loading from the current working directory.Let me look more broadly at all files that interact with GitHub or shell commands in the scan flow.Let me look at the git history for these files and check for recent changes.Let me look more closely at how the agent module constructs shell commands and whether there's a gap between prompts and other parameters.Let me search for any additional patterns I might have missed — specifically around how the `gh` output or HTTP response could be weaponized.After extensive analysis of both files and the agent execution path, let me focus on the core vulnerability. Let me verify one critical aspect of the data flow.## Vulnerability Report: Argument Injection in `gh` CLI Commands via Unsanitized URL Path Components

### Location
`internal/cli/scan.go:224-226` (URL parsing), lines 229-231 and 250 (command construction)

### Vulnerable Code

```go
// Line 219-226: URL parsing with no validation
parts := strings.Split(strings.TrimPrefix(prURL, "https://github.com/"), "/")
if len(parts) < 4 {
    return nil, fmt.Errorf("invalid PR URL format")
}
owner := parts[0]
repo := parts[1]
number := parts[3]  // No validation - can be any string including "--flag"

// Line 229-231: Unsanitized `number` passed as first positional arg to gh
metaCmd := exec.Command("gh", "pr", "view", number, "--repo", owner+"/"+repo,
    "--json", "title,body,author,baseRefName,files",
    "--jq", `{title: .title, body: .body, ...}`)

// Line 250: Same pattern for diff command
diffCmd := exec.Command("gh", "pr", "diff", number, "--repo", owner+"/"+repo)
```

The identical vulnerable pattern exists in `loadWithGH` at lines 493-502.

### Explanation

The `number` value is extracted directly from URL path position `[3]` without any validation (e.g., checking it's numeric). When this value starts with `--`, the `gh` CLI (which uses cobra/pflag) interprets it as a flag rather than a positional argument. This is **argument injection**: while `exec.Command` correctly prevents shell metacharacter injection, it does NOT prevent the arguments themselves from being reinterpreted as flags by the target program.

The critical gap is between the **parallel implementations**:
- `loadPRInfo` (line 229): passes `number` as a raw argument before `--repo` — argument injection shifts flag parsing
- `loadWithGH` (line 500): identical pattern, same vulnerability
- `loadWithHTTP` (line 517): bypasses `gh` entirely with `http.Get`, has NO validation at all on URL structure

The `loadPRInfo` and `loadWithGH` paths enforce a `len(parts) < 4` check but never validate that `number` is actually a number. The `loadWithHTTP` fallback enforces zero structural validation — just `strings.Replace` on the raw URL.

### Exploitation Scenario

**Step 1**: Attacker crafts a URL where the PR number position contains a flag: `https://github.com/owner/repo/pull/--jq`

**Step 2**: User runs: `conclave --claude scan https://github.com/owner/repo/pull/--jq`

**Step 3**: `isPullRequestURL` returns true (contains `/pull/`). `loadPRInfo` parses `number = "--jq"`.

**Step 4**: The constructed command becomes:
```
gh pr view --jq --repo owner/repo --json title,body,author,baseRefName,files --jq {hardcoded_filter}
```

**Step 5**: cobra/pflag parses the first `--jq` flag and **consumes `--repo` as its value** (because `--jq` is a string flag that requires a value argument). This means:
- `--jq` = `"--repo"` (consumed as jq expression value)
- `owner/repo` becomes a positional argument (PR identifier)
- `--json` works normally
- Second `--jq` overwrites the first with the hardcoded filter
- **`--repo` is never set as a flag** — `gh` defaults to current directory's git context

**Step 6**: `gh pr view` now operates on the **user's current directory repository** rather than the URL-specified `owner/repo`. If the user is in a private repository directory and the positional argument `owner/repo` matches a branch name with an open PR, `gh` fetches that private repo's PR data.

**Step 7**: The private PR data (title, body, author, file list, full diff) flows into the scan pipeline → LLM prompts → final report. If `--gist` was specified, this private data is uploaded to GitHub as a gist.

### What the Attacker Gains

The attacker can manipulate which repository the `gh` CLI queries by absorbing the `--repo` flag through argument injection. This causes:
1. **Wrong-repository data exposure**: Private repo PR data substituted into scan results that may be shared or uploaded via `--gist`
2. **Scan integrity bypass**: Security analysis operates on unintended code, potentially generating false "clean" reports for malicious PRs
3. **Browser opening**: With `number = "--web"`, forces `gh` to open a URL in the user's browser (potential phishing vector when combined with the repo context confusion)

### Severity: **High**

This qualifies as **second-order command injection with a realistic trigger**:
- **Second-order**: User-provided URL data is parsed (first order) then injected into `gh` CLI arguments (second order) where it's reinterpreted as flags
- **Realistic trigger**: User clicks a link and runs `conclave scan` on it — a normal workflow
- **Impact**: Broken access control where the tool operates on the wrong repository, potentially exposing private data through the `--gist` upload path

The vulnerability exists because `number`, `owner`, and `repo` undergo **zero input validation** before being passed to `exec.Command`. At minimum, `number` should be validated as numeric, and all values should be preceded by `--` (end-of-flags marker) to prevent flag interpretation.
