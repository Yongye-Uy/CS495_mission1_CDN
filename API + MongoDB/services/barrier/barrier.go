package barrier

import "sync"

// MutationBarrier provides reader-writer synchronization for distributed file operations.
// File mutating operations (Upload, Delete, Recovery) hold a read lock (RLock/RUnlock).
// The final node onboarding activation holds an exclusive write lock (Lock/Unlock).
type MutationBarrier struct {
	mu sync.RWMutex
}

func NewMutationBarrier() *MutationBarrier {
	return &MutationBarrier{}
}

// RLock is acquired by file mutating operations (Upload, Delete, Recovery).
func (b *MutationBarrier) RLock() {
	b.mu.RLock()
}

// RUnlock is released by file mutating operations.
func (b *MutationBarrier) RUnlock() {
	b.mu.RUnlock()
}

// Lock is acquired exclusively by the node onboarding engine during final barrier activation.
func (b *MutationBarrier) Lock() {
	b.mu.Lock()
}

// Unlock releases the exclusive barrier lock.
func (b *MutationBarrier) Unlock() {
	b.mu.Unlock()
}
