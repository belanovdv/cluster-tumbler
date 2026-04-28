// Package store — in-memory snapshot ETCD дерева.
package store

import (
	"strings"
	"sync"
)

// Store хранит KV ETCD snapshot.
type Store struct {
	mu       sync.RWMutex
	data     map[string][]byte
	revision int64
	ready    bool
}

func New() *Store {
	return &Store{
		data: make(map[string][]byte),
	}
}

// Set — обновление ключа
func (s *Store) Set(key string, value []byte, rev int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data[key] = value
	s.revision = rev
}

// Delete — удаление ключа
func (s *Store) Delete(key string, rev int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.data, key)
	s.revision = rev
}

// Snapshot возвращает копию
func (s *Store) Snapshot() (map[string][]byte, int64, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cp := make(map[string][]byte, len(s.data))
	for k, v := range s.data {
		cp[k] = v
	}

	return cp, s.revision, s.ready
}

func (s *Store) MarkReady() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ready = true
}

// Prefix — получить поддерево
func (s *Store) Prefix(prefix string) map[string][]byte {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make(map[string][]byte)
	for k, v := range s.data {
		if strings.HasPrefix(k, prefix) {
			out[k] = v
		}
	}
	return out
}
