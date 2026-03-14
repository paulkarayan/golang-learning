package main

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// --- BUG: check-goroutines (leak) ---
// Dispatcher spawns a goroutine that blocks on results forever with no stop signal.
type Dispatcher struct {
	results chan string
	mu      sync.Mutex
	// Protected by mu:
	running bool
}

func NewDispatcher() *Dispatcher {
	return &Dispatcher{results: make(chan string)}
}

// Start spawns a goroutine that will block on results indefinitely.
// BUG(check-goroutines): no context, no done channel — goroutine leaks if nobody sends.
func (d *Dispatcher) Start() {
	d.mu.Lock()
	d.running = true
	d.mu.Unlock()

	go func() {
		for r := range d.results { // blocks forever if d.results never closed
			fmt.Println("result:", r)
		}
	}()
}

// --- BUG: check-channels ---
// Pipeline has a channel that is closed twice under certain paths.
type Pipeline struct {
	done chan struct{}
	once sync.Once
}

func NewPipeline() *Pipeline {
	return &Pipeline{done: make(chan struct{})}
}

func (p *Pipeline) Shutdown() {
	close(p.done) // BUG(check-channels): no once.Do guard — double-close if called twice
}

// SendAfterClose demonstrates a send on a closed channel.
func SendAfterClose(ch chan<- int) {
	close(ch)
	ch <- 1 // BUG(check-channels): send after close
}

// --- BUG: check-ownership (comment lie) ---
// Job mirrors the teleport pattern: comments claim mutex protection but Stop reads
// stopped without holding the lock first.
type Job struct {
	cmd string

	// No lock needed:
	id string

	// Protected by Manager.mu:
	status  string
	result  int
	stopped bool
}

type Manager struct {
	mu   sync.RWMutex
	jobs map[string]*Job
}

func (m *Manager) Run(j *Job) {
	m.mu.Lock()
	m.jobs[j.id] = j
	m.mu.Unlock()

	go m.execute(j)
}

func (m *Manager) execute(j *Job) {
	time.Sleep(100 * time.Millisecond)
	m.mu.Lock()
	j.status = "done"
	j.result = 42
	j.stopped = false
	m.mu.Unlock()
}

// BUG(check-ownership): reads j.stopped without acquiring m.mu first.
func (m *Manager) Stop(ctx context.Context, j *Job) error {
	if j.stopped { // race: read without lock
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	j.stopped = true
	return nil
}

func main() {
	fmt.Println("buggy_app: this binary exists only as a test fixture")
}
