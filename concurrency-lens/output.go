package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// CheckResult is the top-level JSON envelope every check writes.
type CheckResult struct {
	Check      string            `json:"check"`
	ProjectDir string            `json:"project_dir"`
	RunAt      time.Time         `json:"run_at"`
	DurationMs int64             `json:"duration_ms"`
	ItemCount  int               `json:"item_count"`
	Items      []json.RawMessage `json:"items"`
	Error      string            `json:"error,omitempty"`
}

// OutputFileName derives: <projectname>_<checkname>_<YYYYMMDD-HHMMSS>.json
func OutputFileName(projectDir, check string, t time.Time) string {
	project := filepath.Base(filepath.Clean(projectDir))
	ts := t.Format("20060102-150405")
	// strip "check-" prefix for shorter filenames
	name := check
	if len(name) > 6 && name[:6] == "check-" {
		name = name[6:]
	}
	return fmt.Sprintf("%s_%s_%s.json", project, name, ts)
}

// WriteCheckResult serialises result to outputPath with indentation.
func WriteCheckResult(result CheckResult, outputPath string) error {
	f, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(result)
}

// MarshalItem marshals any value to json.RawMessage (panics never; falls back to null).
func MarshalItem(v interface{}) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`null`)
	}
	return b
}

// absPath returns absolute path; falls back to the original if it fails.
func absPath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return abs
}
