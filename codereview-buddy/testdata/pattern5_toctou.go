package testdata

import "sync"

// Pattern 5: TOCTOU on map lookups
// BUG: Get() returns a pointer, but the entry can be deleted between
// Get() and the caller using the returned value.

type Registry struct {
	mu    sync.Mutex
	items map[string]*Item
}

type Item struct {
	Value string
	done  bool
}

func (r *Registry) Get(key string) *Item {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.items[key] // returns pointer — caller uses it outside lock
}

func (r *Registry) Delete(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if item, ok := r.items[key]; ok {
		item.done = true
		delete(r.items, key)
	}
}

// BUG: Between Get() and Send(), another goroutine can call Delete(),
// making the item invalid.
func (r *Registry) Send(key string, val string) {
	item := r.Get(key)
	if item == nil {
		return
	}
	// TOCTOU gap: item could be deleted and marked done here
	item.Value = val // data race: no lock held
}
