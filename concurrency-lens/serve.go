package main

import (
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

//go:embed ui/index.html
var uiFS embed.FS

// --- run state ---

type RunStatus string

const (
	RunStatusRunning RunStatus = "running"
	RunStatusDone    RunStatus = "done"
	RunStatusError   RunStatus = "error"
)

type RunState struct {
	mu         sync.Mutex
	RunID      string       `json:"run_id"`
	Check      string       `json:"check"`
	ProjectDir string       `json:"project_dir"`
	Status     RunStatus    `json:"status"`
	StartedAt  time.Time    `json:"started_at"`
	OutputFile string       `json:"output_file,omitempty"`
	Result     *CheckResult `json:"result,omitempty"`
	Error      string       `json:"error,omitempty"`
}

var (
	runsMu     sync.RWMutex
	runsMap    = make(map[string]*RunState)
	serveResultsDir string
)

// --- check metadata (served to UI) ---

type CheckMeta struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Group       string `json:"group"` // mechanical | llm_enhanced
}

var availableChecks = []CheckMeta{
	{"check-closure", "Closure Capture",
		"Goroutines that close over loop variables without passing them as arguments.",
		"mechanical"},
	{"check-maps", "Concurrent Maps",
		"Maps written inside goroutines without synchronisation.",
		"mechanical"},
	{"check-wg", "WaitGroup Misuse",
		"WaitGroup.Add() called inside goroutine body — race with wg.Wait().",
		"mechanical"},
	{"check-defer-unlock", "Defer Unlock in Loop",
		"defer mu.Unlock() inside a for loop holds the lock until the function returns.",
		"mechanical"},
	{"check-ownership", "Variable Ownership",
		"Catalog every struct field and package var: who owns it, what protects it, green/yellow/red.",
		"llm_enhanced"},
	{"check-goroutines", "Goroutine Graph",
		"Lifecycle and leak risk for every spawned goroutine; builds goroutine + edge tables.",
		"llm_enhanced"},
	{"check-channels", "Channel Lifecycle",
		"Send-after-close, deadlock, and buffer saturation analysis per channel.",
		"llm_enhanced"},
	{"check-locks", "Lock Ordering",
		"Identifies functions that acquire the same mutexes in conflicting orders.",
		"llm_enhanced"},
}

// --- serve command ---

func runServe(args []string) error {
	fs2 := flag.NewFlagSet("serve", flag.ExitOnError)
	port := fs2.Int("port", 8765, "HTTP port")
	resultsDir := fs2.String("results-dir", "", "directory for output JSON files (default: cwd)")
	if err := fs2.Parse(args); err != nil {
		return err
	}

	if *resultsDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		*resultsDir = cwd
	}
	serveResultsDir = *resultsDir

	mux := http.NewServeMux()

	// UI
	subFS, err := fs.Sub(uiFS, "ui")
	if err != nil {
		return err
	}
	mux.Handle("GET /", http.FileServer(http.FS(subFS)))

	// API
	mux.HandleFunc("GET /api/checks", handleGetChecks)
	mux.HandleFunc("POST /api/run", handlePostRun)
	mux.HandleFunc("GET /api/run/{id}", handleGetRun)
	mux.HandleFunc("GET /api/results", handleGetResults)
	mux.HandleFunc("GET /api/result", handleGetResult)

	addr := fmt.Sprintf(":%d", *port)
	url := fmt.Sprintf("http://localhost:%d", *port)
	fmt.Fprintf(os.Stderr, "concurrency-lens serving at %s\n", url)
	fmt.Fprintf(os.Stderr, "results directory: %s\n", serveResultsDir)

	go func() {
		time.Sleep(300 * time.Millisecond)
		openBrowser(url)
	}()

	return http.ListenAndServe(addr, mux)
}

// --- HTTP handlers ---

func handleGetChecks(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, availableChecks)
}

type runRequest struct {
	Check      string `json:"check"`
	ProjectDir string `json:"project_dir"`
}

func handlePostRun(w http.ResponseWriter, r *http.Request) {
	var req runRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Check == "" || req.ProjectDir == "" {
		writeError(w, http.StatusBadRequest, "check and project_dir are required")
		return
	}

	runID := fmt.Sprintf("%d", time.Now().UnixNano())
	state := &RunState{
		RunID:      runID,
		Check:      req.Check,
		ProjectDir: req.ProjectDir,
		Status:     RunStatusRunning,
		StartedAt:  time.Now(),
	}

	runsMu.Lock()
	runsMap[runID] = state
	runsMu.Unlock()

	go executeCheck(state)

	w.WriteHeader(http.StatusAccepted)
	writeJSON(w, map[string]string{"run_id": runID})
}

func handleGetRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	runsMu.RLock()
	state, ok := runsMap[id]
	runsMu.RUnlock()
	if !ok {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	writeJSON(w, state)
}

type resultMeta struct {
	File      string    `json:"file"`
	Check     string    `json:"check"`
	RunAt     time.Time `json:"run_at"`
	ItemCount int       `json:"item_count"`
	Error     string    `json:"error,omitempty"`
}

func handleGetResults(w http.ResponseWriter, r *http.Request) {
	dir := r.URL.Query().Get("dir")
	if dir == "" {
		dir = serveResultsDir
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("cannot read dir: %v", err))
		return
	}

	var metas []resultMeta
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var cr CheckResult
		if err := json.Unmarshal(data, &cr); err != nil {
			continue
		}
		// Only include files that look like our output
		if cr.Check == "" {
			continue
		}
		metas = append(metas, resultMeta{
			File:      path,
			Check:     cr.Check,
			RunAt:     cr.RunAt,
			ItemCount: cr.ItemCount,
			Error:     cr.Error,
		})
	}

	writeJSON(w, metas)
}

func handleGetResult(w http.ResponseWriter, r *http.Request) {
	file := r.URL.Query().Get("file")
	if file == "" {
		writeError(w, http.StatusBadRequest, "file query param required")
		return
	}
	data, err := os.ReadFile(file)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("cannot read file: %v", err))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

// --- check executor ---

func executeCheck(state *RunState) {
	start := time.Now()
	projectDir := state.ProjectDir

	var items []json.RawMessage
	var runErr error

	switch state.Check {
	case "check-closure":
		findings, err := checkClosure(projectDir)
		runErr = err
		for _, f := range findings {
			items = append(items, MarshalItem(f))
		}
	case "check-maps":
		findings, err := checkMaps(projectDir)
		runErr = err
		for _, f := range findings {
			items = append(items, MarshalItem(f))
		}
	case "check-wg":
		findings, err := checkWG(projectDir)
		runErr = err
		for _, f := range findings {
			items = append(items, MarshalItem(f))
		}
	case "check-defer-unlock":
		findings, err := checkDefer(projectDir)
		runErr = err
		for _, f := range findings {
			items = append(items, MarshalItem(f))
		}
	case "check-ownership":
		findings, err := checkOwnership(projectDir, true)
		runErr = err
		for _, f := range findings {
			items = append(items, MarshalItem(f))
		}
	case "check-goroutines":
		rawItems, err := checkGoroutines(projectDir, true)
		runErr = err
		items = rawItems
	case "check-channels":
		findings, err := checkChannels(projectDir, true)
		runErr = err
		for _, f := range findings {
			items = append(items, MarshalItem(f))
		}
	case "check-locks":
		findings, err := checkLocks(projectDir, true)
		runErr = err
		for _, f := range findings {
			items = append(items, MarshalItem(f))
		}
	default:
		runErr = fmt.Errorf("unknown check: %s", state.Check)
	}

	if items == nil {
		items = []json.RawMessage{}
	}

	result := CheckResult{
		Check:      state.Check,
		ProjectDir: absPath(projectDir),
		RunAt:      start,
		DurationMs: time.Since(start).Milliseconds(),
		ItemCount:  len(items),
		Items:      items,
	}
	if runErr != nil {
		result.Error = runErr.Error()
	}

	outPath := filepath.Join(serveResultsDir, OutputFileName(projectDir, state.Check, start))
	_ = WriteCheckResult(result, outPath)

	state.mu.Lock()
	defer state.mu.Unlock()
	state.Result = &result
	state.OutputFile = outPath
	if runErr != nil {
		state.Status = RunStatusError
		state.Error = runErr.Error()
	} else {
		state.Status = RunStatusDone
	}
}

// --- helpers ---

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func openBrowser(url string) {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd, args = "open", []string{url}
	case "linux":
		cmd, args = "xdg-open", []string{url}
	case "windows":
		cmd, args = "rundll32", []string{"url.dll,FileProtocolHandler", url}
	default:
		return
	}
	exec.Command(cmd, args...).Start() //nolint:errcheck
}
