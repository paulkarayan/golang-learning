package main

import (
	"context"
	"fmt"
	"sync"
)

// using mutex here to protect the station map.
type StationManager struct {
	mu       Mutex
	stations map[string]*Broadcaster
}

func NewStationManager() *StationManager {
	return &StationManager{
		stations: make(map[string]*Broadcaster),
	}
}

func (sm *StationManager) Create(id string) error {
	// lock, check if exists,
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if _, ok := sm.stations[id]; ok {
		return fmt.Errorf("station %s already exists", id)
	}

	// create broadcaster
	sm.stations[id] = NewBroadcaster()
	// Go functions that return error return nil on success
	return nil
}

func (sm *StationManager) Get(id string) (*Broadcaster, bool) {
	// lock
	sm.mu.Lock()
	defer sm.mu.Unlock()
	// lookup
	b, ok := sm.stations[id]
	return b, ok
}

func (sm *StationManager) Stop(id string) error {
	// lock
	sm.mu.Lock()
	defer sm.mu.Unlock()
	// close broadcaster after finding by id
	b, ok := sm.stations[id]
	if !ok {
		return fmt.Errorf("station %s not found", id)
	}
	b.Close()
	// delete from map
	delete(sm.stations, id)
	return nil

}

// use a sync.Cond pattern instead.
// state is in one shared byte buffer and one sync.Cond var per job
// Cond provides a mutex to protect the buffer
// note we dont need to keep subs or subs nextid anymore
type Broadcaster struct {
	mu      Mutex
	cond    *sync.Cond
	history [][]byte
	done    bool
}

// create Cond variable & associated mutex
func NewBroadcaster() *Broadcaster {
	b := &Broadcaster{}
	b.cond = sync.NewCond(&b.mu)
	return b
}

func (b *Broadcaster) Send(data []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// ensure the process hasn't terminated already
	if b.done {
		return
	}
	// otherwise, append your data to the history buffer
	// and tell the clients about it via Broadcast
	b.history = append(b.history, data)
	b.cond.Broadcast()
}

func (b *Broadcaster) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	// if the process has terminated, set the done flag
	if b.done {
		return
	}
	b.done = true

	// then wake all the clients to tell them
	b.cond.Broadcast()
}

// IMPORTANT: the caller is responsible for behavior when context is cancelled, which would
// happen if client disconnects (like request context cancels).
// we'll set that up in the http handler. otherwise there's a goroutine leak
func (b *Broadcaster) Read(ctx context.Context, cursor *int) ([]byte, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// wait until there's new data, we're done, or context is cancelled
	for *cursor >= len(b.history) && !b.done {
		if ctx.Err() != nil {
			return nil, false
		}
		b.cond.Wait()
	}

	// data available — return it
	if *cursor < len(b.history) {
		data := b.history[*cursor]
		*cursor++
		return data, true
	}

	// done and fully drained
	return nil, false
}
