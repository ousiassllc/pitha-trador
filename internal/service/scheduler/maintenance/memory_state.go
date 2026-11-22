package maintenance

import (
	"context"
	"sync"
	"time"
)

// memoryState is the State used when none is configured.
type memoryState struct {
	mu sync.Mutex
	m  map[string]string
}

func newMemoryState() *memoryState { return &memoryState{m: make(map[string]string)} }

func (s *memoryState) Get(_ context.Context, key string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[key]
	return v, ok, nil
}

func (s *memoryState) Set(_ context.Context, key, value string, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[key] = value
	return nil
}
