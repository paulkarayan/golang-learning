---
id: ce6e4d8e-44a1-4153-a8df-017d71a94ba9
name: Conclave CLI Agent Orchestration Tool
created: 2026-03-07T20:02:03.371268-08:00
codebase_root: /Users/pk/golang-learning/dj-broadcast
agent: claude
---

# Codebase Analysis: Conclave CLI Agent Orchestration Tool

## Overview

Conclave is a Go CLI tool that orchestrates multiple LLM agents (Claude, Codex, Gemini) to perform systematic security audits of codebases. It coordinates a multi-stage pipeline: planning (codebase analysis), assessment (parallel agent review), adversarial convene (steel man/critique/judge debate), and synthesis of final reports. The tool executes external CLI processes (claude, codex, gemini, gh) via shell commands, manages state as markdown files with YAML frontmatter in a `.conclave/` directory, and optionally serves a real-time WebSocket-based web dashboard.

The architecture involves several security-relevant boundaries: shell command execution for spawning LLM agent subprocesses, a WebSocket server for the web dashboard with agent control capabilities (kill commands), external GitHub API integration via the `gh` CLI and HTTP requests, file system operations for state persistence, and configuration loading from user home directory. Prompts containing user-supplied and LLM-generated content flow through Go templates and are passed as shell arguments to external processes.

The tool uses `--dangerously-skip-permissions` for Claude CLI, `-y` (yolo mode) for Gemini, and `--sandbox workspace-write` for Codex, representing different trust models for the spawned agents. The web dashboard accepts commands from browser clients that can kill running agent processes.

## Subsystems

### shell-execution
**Name:** Shell Command Execution and Agent Spawning
**Paths:** internal/agent/claude.go, internal/agent/codex.go, internal/agent/gemini.go, internal/agent/agent.go
**Description:** Spawns external CLI processes (claude, codex, gemini) via `bash -lc` with user-supplied prompts passed as shell arguments. Handles shellQuote escaping, constructs command-line arguments including `--dangerously-skip-permissions`, manages stdout/stderr streaming, and parses JSON streaming output. Each agent implementation builds shell commands differently and uses different security models (permissions skip, yolo mode, sandbox).
**Interactions:** prompt-construction, streaming-orchestration, config-management

### prompt-construction
**Name:** Prompt Generation and Template Rendering
**Paths:** internal/prompts/prompts.go, internal/prompts/*.txt, internal/assess/assess.go, internal/assess/perspective.go, internal/convene/convene.go, internal/plan/generator.go, internal/plan/parser.go
**Description:** Generates prompts using Go text/template with data from plans, subsystems, and prior agent outputs. Agent output from one phase is embedded into prompts for subsequent phases (e.g., assessment findings into steel man prompts, steel man output into critique prompts). Custom user instructions from config are also injected into prompts. Template rendering uses text/template which does not auto-escape.
**Interactions:** shell-execution, state-persistence, config-management

### web-dashboard
**Name:** WebSocket Web Dashboard and Agent Control
**Paths:** internal/web/server.go, internal/web/hub.go, internal/web/static/index.html
**Description:** HTTP server with WebSocket endpoint for real-time monitoring. Accepts all origins (CheckOrigin returns true). Receives control commands from browser clients (kill, kill_all) that cancel agent process contexts. Serves embedded static files and broadcasts agent status, logs, and phase updates to all connected clients. The hub manages client registration, message broadcasting, and command dispatch.
**Interactions:** streaming-orchestration, shell-execution

### state-persistence
**Name:** File System State Management
**Paths:** internal/state/state.go
**Description:** Manages the `.conclave/` directory structure with plans, assessments, verdicts, debates, and results stored as markdown files with YAML frontmatter. Performs file read/write operations, YAML marshaling/unmarshaling, and path construction using plan IDs and subsystem slugs. Plan IDs are truncated UUIDs used in directory paths. File parsing handles frontmatter extraction with string manipulation.
**Interactions:** prompt-construction, cli-commands

### config-management
**Name:** Configuration Loading and Runtime Config
**Paths:** internal/config/config.go, internal/config/loader.go, internal/config/parser.go, internal/config/runtime.go
**Description:** Loads YAML configuration from `~/.conclave/config.yaml`, parses model specifications (provider + model + effort), validates provider names, and builds RuntimeConfig for agent creation. Supports named profiles with per-stage agent configuration. ParseModelSpec splits user-provided strings into provider/model/effort components. Config instructions are injected into assessment prompts.
**Interactions:** prompt-construction, shell-execution, cli-commands

### github-integration
**Name:** GitHub API and CLI Integration
**Paths:** internal/cli/scan.go, internal/scan/scan.go
**Description:** Fetches PR metadata, diffs, and issue content from GitHub using the `gh` CLI and direct HTTP API calls. Parses GitHub URLs to extract owner/repo/number, constructs `gh` CLI commands with `--jq` filters, and falls back to unauthenticated HTTP API requests. Includes custom JSON parsing (extractJSONString, extractJSONArray) for `gh` output. Creates GitHub gists with `gh gist create` for report sharing.
**Interactions:** shell-execution, prompt-construction, cli-commands

### streaming-orchestration
**Name:** Agent Streaming, Resilience, and Pipeline Orchestration
**Paths:** internal/agent/stream.go, internal/agent/resilient.go, internal/agent/pipeline.go, internal/agent/registry.go, internal/agent/usage.go
**Description:** Manages parallel agent execution with streaming output, failover between providers, pipelined adversarial debate (steel man → critique → judge per finding), agent lifecycle registry with kill capabilities, and usage/cost tracking. ResilientAgent passes accumulated partial output from failed agents to retry agents as context. The registry stores CancelFunc references for process termination.
**Interactions:** shell-execution, web-dashboard, cli-commands

### cli-commands
**Name:** CLI Command Handlers and Orchestration Flow
**Paths:** internal/cli/root.go, internal/cli/run.go, internal/cli/plan.go, internal/cli/assess.go, internal/cli/convene.go, internal/cli/complete.go, internal/cli/status.go, internal/cli/review.go, internal/cli/scan.go, cmd/conclave/main.go
**Description:** Cobra command handlers that wire together the full audit pipeline. Manages provider selection, agent distribution, subsystem selection (random unreviewed), verdict parsing, gist creation, and browser opening. The run command orchestrates plan→assess→convene→complete. The review command reads file paths from stdin. The scan command handles PR and vulnerability scanning flows.
**Interactions:** config-management, state-persistence, streaming-orchestration, github-integration, prompt-construction
