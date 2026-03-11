---
id: 9dfbc305-8131-4430-ae62-c6396230f966
name: dj-broadcast
created: 2026-03-07T21:16:08.466505-08:00
codebase_root: /Users/pk/golang-learning/dj-broadcast
agent: claude
---

# Codebase Analysis: dj-broadcast

## Overview

dj-broadcast is a Go HTTP server implementing a real-time broadcasting system where "DJs" can create named stations, send data (text or binary) to them, and multiple listeners can subscribe to receive the stream. It uses an in-memory architecture with no persistence, no authentication, and no TLS. The server listens on port 8080 and exposes four REST endpoints for station CRUD and streaming operations.

The concurrency model centers on a `sync.Cond`-based broadcast pattern. Each station has a `Broadcaster` that maintains a shared history buffer protected by a mutex. Readers use cursor-based polling with `cond.Wait()` to block until new data arrives. The `StationManager` holds a map of station IDs to broadcasters, also mutex-protected. The codebase has extensive concurrency testing including stress tests, race detection, deadlock detection (via swappable mutex implementation), and goroutine leak detection via `goleak`.

The project is a learning exercise with no external dependencies beyond testing utilities (`goleak`, `go-deadlock`). There is no authentication, authorization, rate limiting, input validation beyond checking for empty IDs, or request body size limits. All state is in-memory and the HTTP API is unauthenticated.

## Subsystems

### http-api
**Name:** HTTP API Layer
**Paths:** main.go
**Description:** Defines the HTTP server and four route handlers: POST /station (create), POST /station/broadcast (send data), GET /station/listen (SSE-style streaming subscription), DELETE /station (teardown). Handles request parsing, query parameter extraction, response writing, and HTTP streaming via Flusher. Contains the `Send` method on StationManager that atomically checks station existence and sends data to avoid TOCTOU. Reads entire request body with `io.ReadAll` without size limits.
**Interactions:** station-manager, broadcaster

### station-manager
**Name:** Station Manager
**Paths:** broadcaster.go:10-48
**Description:** Thread-safe registry of named broadcast stations. Uses a mutex-protected map to manage station lifecycle (Create, Get, Stop). Implements lazy deletion of stopped stations on next Get() call. Provides the coordination layer between HTTP handlers and individual broadcasters.
**Interactions:** http-api, broadcaster

### broadcaster
**Name:** Broadcaster Core
**Paths:** broadcaster.go:65-135
**Description:** The core concurrency primitive. Each Broadcaster holds a mutex-protected history buffer (unbounded `[][]byte` slice) and a `sync.Cond` for notifying waiting readers. Provides Send (append + broadcast), Read (cursor-based blocking read with context cancellation support), and Close (set done flag + wake all waiters). The history buffer grows without bound — no eviction or size cap.
**Interactions:** station-manager, http-api

### mutex-abstraction
**Name:** Swappable Mutex (Deadlock Detection)
**Paths:** mutex.go, mutex_deadlock.go
**Description:** Build-tag-based mutex type alias that swaps between standard `sync.Mutex` and `go-deadlock.Mutex` for runtime deadlock detection. Used by both StationManager and Broadcaster. Enabled via `-tags=deadlock` build flag.
**Interactions:** broadcaster, station-manager

### testing-infrastructure
**Name:** Test Suite and CI Pipeline
**Paths:** main_test.go, broadcaster_test.go, broadcaster_stress_test.go, Makefile, .semgrep.yml, .golangci.yml
**Description:** Comprehensive test infrastructure including unit tests, HTTP integration tests, stress/race tests (behind `stress` build tag), goroutine leak detection via `goleak.VerifyTestMain`, and a CI pipeline with linting, vetting, semgrep custom rules, race detection, and deadlock detection. The Makefile also includes LLM-based code review targets and integration with an external `codereview-buddy` tool.
**Interactions:** http-api, station-manager, broadcaster
