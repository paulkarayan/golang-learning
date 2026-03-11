// Package cgroup provides a minimal cgroups v2 interface for per-job
// resource control. It writes directly to /sys/fs/cgroup/ — no third-party deps.
//
// Usage:
//
//	cg, err := cgroup.Create("job-123", cgroup.Resources{
//	    MemoryMax: 100 * 1024 * 1024,  // 100MB
//	    CPUMax:    "50000 100000",       // 50% of one core
//	    IOMax:     "8:0 wbps=1048576",   // 1MB/s write to sda
//	})
//	defer cg.Destroy()
//
//	err = cg.AddProcess(cmd.Process.Pid)
package cgroup

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const cgroupRoot = "/sys/fs/cgroup"

// Resources defines the resource limits for a cgroup.
type Resources struct {
	// MemoryMax is the hard memory limit in bytes. 0 means no limit.
	MemoryMax int64

	// CPUMax is the cpu.max value, e.g. "50000 100000" for 50% of one core.
	// Format: "$MAX $PERIOD" in microseconds. Empty means no limit.
	CPUMax string

	// IOMax is the io.max value, e.g. "8:0 wbps=1048576" to limit writes
	// on device 8:0 to 1MB/s. Empty means no limit.
	IOMax string
}

// Cgroup represents a cgroups v2 control group.
type Cgroup struct {
	name string
	path string
}

// Create makes a new cgroup under /sys/fs/cgroup/<name> and configures
// the given resource limits. The cgroup must not already exist.
func Create(name string, res Resources) (*Cgroup, error) {
	if name == "" {
		return nil, errors.New("cgroup name cannot be empty")
	}
	if strings.Contains(name, "/") || strings.Contains(name, "..") {
		return nil, fmt.Errorf("invalid cgroup name: %q", name)
	}

	path := filepath.Join(cgroupRoot, name)

	if err := os.Mkdir(path, 0755); err != nil {
		return nil, fmt.Errorf("creating cgroup dir: %w", err)
	}

	cg := &Cgroup{name: name, path: path}

	if err := cg.configure(res); err != nil {
		// Clean up on failure
		os.Remove(path)
		return nil, fmt.Errorf("configuring cgroup: %w", err)
	}

	return cg, nil
}

// configure writes resource limits to the cgroup control files.
func (cg *Cgroup) configure(res Resources) error {
	if res.MemoryMax > 0 {
		if err := cg.write("memory.max", strconv.FormatInt(res.MemoryMax, 10)); err != nil {
			return fmt.Errorf("setting memory.max: %w", err)
		}
	}

	if res.CPUMax != "" {
		if err := cg.write("cpu.max", res.CPUMax); err != nil {
			return fmt.Errorf("setting cpu.max: %w", err)
		}
	}

	if res.IOMax != "" {
		if err := cg.write("io.max", res.IOMax); err != nil {
			return fmt.Errorf("setting io.max: %w", err)
		}
	}

	return nil
}

// AddProcess moves a process into this cgroup by writing its PID
// to cgroup.procs.
func (cg *Cgroup) AddProcess(pid int) error {
	if err := cg.write("cgroup.procs", strconv.Itoa(pid)); err != nil {
		return fmt.Errorf("adding pid %d to cgroup: %w", pid, err)
	}
	return nil
}

// Pids returns all PIDs currently in this cgroup.
func (cg *Cgroup) Pids() ([]int, error) {
	data, err := os.ReadFile(filepath.Join(cg.path, "cgroup.procs"))
	if err != nil {
		return nil, err
	}

	var pids []int
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		pid, err := strconv.Atoi(line)
		if err != nil {
			return nil, fmt.Errorf("parsing pid %q: %w", line, err)
		}
		pids = append(pids, pid)
	}
	return pids, nil
}

// MemoryCurrent returns the current memory usage in bytes.
func (cg *Cgroup) MemoryCurrent() (int64, error) {
	data, err := os.ReadFile(filepath.Join(cg.path, "memory.current"))
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
}

// OOMEvents returns the number of times the OOM killer has been invoked.
func (cg *Cgroup) OOMEvents() (int64, error) {
	data, err := os.ReadFile(filepath.Join(cg.path, "memory.events"))
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "oom_kill ") {
			parts := strings.Fields(line)
			if len(parts) == 2 {
				return strconv.ParseInt(parts[1], 10, 64)
			}
		}
	}
	return 0, nil
}

// Destroy removes the cgroup. All processes must have exited first.
func (cg *Cgroup) Destroy() error {
	// rmdir removes the cgroup dir; kernel cleans up the control files.
	// This fails if processes are still in the cgroup.
	if err := os.Remove(cg.path); err != nil {
		return fmt.Errorf("destroying cgroup %s: %w", cg.name, err)
	}
	return nil
}

// Path returns the filesystem path of this cgroup.
func (cg *Cgroup) Path() string {
	return cg.path
}

// write writes a value to a cgroup control file.
func (cg *Cgroup) write(file, value string) error {
	return os.WriteFile(filepath.Join(cg.path, file), []byte(value), 0644)
}
