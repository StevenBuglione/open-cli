package app

import (
	"fmt"
	"sync"
	"time"
)

// Item is the core resource served by the test API.
type Item struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Tags      []string  `json:"tags,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// Operation represents a long-running async operation.
type Operation struct {
	ID        string    `json:"id"`
	Status    string    `json:"status"` // pending | running | done | failed
	Progress  int       `json:"progress"`
	CreatedAt time.Time `json:"createdAt"`
}

// Store is an in-memory store for items and operations.
type Store struct {
	mu         sync.RWMutex
	items      map[string]*Item
	operations map[string]*Operation
	nextID     int
	nextOpID   int
}

// NewStore creates a seeded Store.
func NewStore() *Store {
	s := &Store{
		items:      make(map[string]*Item),
		operations: make(map[string]*Operation),
		nextID:     6,
		nextOpID:   1,
	}
	tags := [][]string{
		{"alpha", "beta"},
		{"gamma"},
		{"alpha"},
		{"delta", "epsilon"},
		{"beta", "gamma"},
	}
	for i := 1; i <= 5; i++ {
		id := fmt.Sprintf("item-%d", i)
		s.items[id] = &Item{
			ID:        id,
			Name:      fmt.Sprintf("Item %d", i),
			Tags:      tags[i-1],
			CreatedAt: time.Now().Add(time.Duration(-i) * time.Hour),
		}
	}
	return s
}

func (s *Store) listItems(tag string) []*Item {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*Item, 0, len(s.items))
	for _, it := range s.items {
		if tag == "" || containsTag(it.Tags, tag) {
			result = append(result, it)
		}
	}
	return result
}

func (s *Store) getItem(id string) (*Item, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	it, ok := s.items[id]
	return it, ok
}

func (s *Store) createItem(name string, tags []string) *Item {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := fmt.Sprintf("item-%d", s.nextID)
	s.nextID++
	it := &Item{ID: id, Name: name, Tags: tags, CreatedAt: time.Now()}
	s.items[id] = it
	return it
}

func (s *Store) updateItem(id, name string, tags []string) (*Item, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, ok := s.items[id]
	if !ok {
		return nil, false
	}
	it.Name = name
	it.Tags = tags
	return it, true
}

func (s *Store) deleteItem(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.items[id]
	if ok {
		delete(s.items, id)
	}
	return ok
}

func (s *Store) createOperation() *Operation {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := fmt.Sprintf("op-%d", s.nextOpID)
	s.nextOpID++
	op := &Operation{
		ID:        id,
		Status:    "pending",
		Progress:  0,
		CreatedAt: time.Now(),
	}
	s.operations[id] = op
	// immediately advance to running/done for deterministic tests
	op.Status = "running"
	op.Progress = 50
	return op
}

func (s *Store) getOperation(id string) (*Operation, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	op, ok := s.operations[id]
	return op, ok
}

func containsTag(tags []string, t string) bool {
	for _, tag := range tags {
		if tag == t {
			return true
		}
	}
	return false
}
