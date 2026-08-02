package document

import "sync"

type Store struct {
	mu    sync.RWMutex
	files map[string]string
}

func NewStore() *Store {
	return &Store{files: make(map[string]string)}
}

func (s *Store) Set(path string, content string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files[path] = content
}

func (s *Store) Get(path string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	content, ok := s.files[path]
	return content, ok
}

func (s *Store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.files)
}
