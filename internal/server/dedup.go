package server

import "sync"

// deduper remembers event ids already handled. AIOStreams retries an event with
// the same id, and some clients report one stop twice, so the bridge drops a
// repeat instead of writing it again. The store is bounded and drops the
// oldest ids once it is full.
//
// An id is reserved while its write is in flight, so concurrent duplicates are
// dropped. If the write fails the caller must release the reservation with
// forget, otherwise the retry AIOStreams sends with the same id would be
// mistaken for a duplicate and acknowledged without being applied.
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

// first reserves id and reports whether it had not been seen before. A caller
// that fails to apply the event must call forget so a retry is not dropped.
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

// forget releases a reservation so a later retry with the same id can apply.
func (d *deduper) forget(id string) {
	if id == "" {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.seen[id]; !ok {
		return
	}
	delete(d.seen, id)
	for i, existing := range d.order {
		if existing == id {
			d.order = append(d.order[:i], d.order[i+1:]...)
			break
		}
	}
}
