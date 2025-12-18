package handlers

import "sync"

type cancelRegistry struct {
	mu sync.Mutex
	m  map[string]func()
}

func newRegistry() *cancelRegistry {
	return &cancelRegistry{m: make(map[string]func())}
}

func (r *cancelRegistry) Add(id string, cancel func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.m[id] = cancel
}

func (r *cancelRegistry) Cancel(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if cancel, ok := r.m[id]; ok {
		cancel()
		delete(r.m, id)
		return true
	}
	return false
}

func (r *cancelRegistry) Remove(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.m, id)
}
