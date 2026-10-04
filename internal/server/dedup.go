package server

import "sync"

// deduper remembers event ids already handled. AIOStreams retries an event with
// the same id, and some clients report one stop twice, so the bridge drops a
// repeat instead of writing it again. The store is bounded and drops the
// oldest ids once it is full.
type deduper struct {
	mu       sync.Mutex
	seen     map[string]struct{}
	order    []string
	capacity int
}

func newDeduper(capacity int) *deduper {
	if capacity <= 0 {
		capacity = 4096
	}
	return &deduper{seen: make(map[string]struct{}), capacity: capacity}
}

// first reports whether id has not been seen before, and records it.
func (d *deduper) first(id string) bool {
	if id == "" {
		return true
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.seen[id]; ok {
		return false
	}
	d.seen[id] = struct{}{}
	d.order = append(d.order, id)
	if len(d.order) > d.capacity {
		oldest := d.order[0]
		d.order = d.order[1:]
		delete(d.seen, oldest)
	}
	return true
}
