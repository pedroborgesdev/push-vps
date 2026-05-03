package session

import (
	"sync"
	"time"
)

const (
	MaxMessages     = 20
	SessionTTL      = time.Hour
	CleanupInterval = 10 * time.Minute
)

type Message struct {
	Role    string
	Content string
}

type SessionData struct {
	Messages   []Message
	LastActive time.Time
}

type Store struct {
	mu       sync.RWMutex
	sessions map[string]*SessionData
}

func NewStore() *Store {
	s := &Store{
		sessions: make(map[string]*SessionData),
	}
	go s.startCleanup()
	return s
}

// GetHistory returns a copy of the session's message history.
func (s *Store) GetHistory(id string) []Message {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sess, ok := s.sessions[id]
	if !ok {
		return nil
	}

	history := make([]Message, len(sess.Messages))
	copy(history, sess.Messages)
	return history
}

// Save appends a user/assistant pair to the session, capping at MaxMessages.
func (s *Store) Save(id, userMsg, aiMsg string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sess, ok := s.sessions[id]
	if !ok {
		sess = &SessionData{
			Messages: make([]Message, 0, MaxMessages),
		}
		s.sessions[id] = sess
	}

	sess.Messages = append(sess.Messages,
		Message{Role: "user", Content: userMsg},
		Message{Role: "assistant", Content: aiMsg},
	)
	sess.LastActive = time.Now()

	if len(sess.Messages) > MaxMessages {
		sess.Messages = sess.Messages[len(sess.Messages)-MaxMessages:]
	}
}

// ClearContext removes all messages from the session, resetting its history.
func (s *Store) ClearContext(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if sess, ok := s.sessions[id]; ok {
		sess.Messages = make([]Message, 0, MaxMessages)
		sess.LastActive = time.Now()
	}
}

func (s *Store) startCleanup() {
	ticker := time.NewTicker(CleanupInterval)
	defer ticker.Stop()
	for range ticker.C {
		s.evictExpired()
	}
}

func (s *Store) evictExpired() {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := time.Now().Add(-SessionTTL)
	for id, sess := range s.sessions {
		if sess.LastActive.Before(cutoff) {
			delete(s.sessions, id)
		}
	}
}
