package session

import (
	"crypto/rand"
	"encoding/base32"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Session represents a game session between host and joiner
type Session struct {
	ID            string    `json:"id"`
	Code          string    `json:"code"`
	HostToken     string    `json:"hostToken,omitempty"`
	JoinToken     string    `json:"joinToken,omitempty"`
	HostConnected bool      `json:"hostConnected"`
	JoinConnected bool      `json:"joinConnected"`
	CreatedAt     time.Time `json:"createdAt"`
	ExpiresAt     time.Time `json:"expiresAt"`
}

// Store is an in-memory session store with TTL
type Store struct {
	mu       sync.RWMutex
	sessions map[string]*Session // keyed by session ID
	byCodes  map[string]string   // code -> session ID
	ttl      time.Duration
}

// NewStore creates a new session store
func NewStore(ttl time.Duration) *Store {
	s := &Store{
		sessions: make(map[string]*Session),
		byCodes:  make(map[string]string),
		ttl:      ttl,
	}
	go s.cleanupLoop()
	return s
}

// Create creates a new session and returns it
func (s *Store) Create() (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	id, err := generateID(16)
	if err != nil {
		return nil, fmt.Errorf("failed to generate session ID: %w", err)
	}

	code, err := generateCode()
	if err != nil {
		return nil, fmt.Errorf("failed to generate join code: %w", err)
	}

	hostToken, err := generateID(32)
	if err != nil {
		return nil, fmt.Errorf("failed to generate host token: %w", err)
	}

	now := time.Now()
	session := &Session{
		ID:        id,
		Code:      code,
		HostToken: hostToken,
		CreatedAt: now,
		ExpiresAt: now.Add(s.ttl),
	}

	s.sessions[id] = session
	s.byCodes[code] = id

	return session, nil
}

// GetByID retrieves a session by ID
func (s *Store) GetByID(id string) (*Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	session, ok := s.sessions[id]
	if !ok || time.Now().After(session.ExpiresAt) {
		return nil, false
	}
	return session, true
}

// GetByCode retrieves a session by join code
func (s *Store) GetByCode(code string) (*Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	id, ok := s.byCodes[code]
	if !ok {
		return nil, false
	}

	session, ok := s.sessions[id]
	if !ok || time.Now().After(session.ExpiresAt) {
		return nil, false
	}
	return session, true
}

// Join generates a join token for an existing session
func (s *Store) Join(code string) (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	id, ok := s.byCodes[code]
	if !ok {
		return nil, fmt.Errorf("session not found")
	}

	session, ok := s.sessions[id]
	if !ok || time.Now().After(session.ExpiresAt) {
		return nil, fmt.Errorf("session expired")
	}

	if session.JoinToken != "" {
		// Already has a joiner
		return nil, fmt.Errorf("session already has a joiner")
	}

	joinToken, err := generateID(32)
	if err != nil {
		return nil, fmt.Errorf("failed to generate join token: %w", err)
	}

	session.JoinToken = joinToken
	return session, nil
}

// SetHostConnected marks the host as connected to relay
func (s *Store) SetHostConnected(id string, connected bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, ok := s.sessions[id]
	if !ok {
		return fmt.Errorf("session not found")
	}
	session.HostConnected = connected
	return nil
}

// SetJoinConnected marks the joiner as connected to relay
func (s *Store) SetJoinConnected(id string, connected bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, ok := s.sessions[id]
	if !ok {
		return fmt.Errorf("session not found")
	}
	session.JoinConnected = connected
	return nil
}

// Delete removes a session
func (s *Store) Delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if session, ok := s.sessions[id]; ok {
		delete(s.byCodes, session.Code)
		delete(s.sessions, id)
	}
}

// ValidateToken checks if a token is valid for a session
func (s *Store) ValidateToken(sessionID, token, role string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	session, ok := s.sessions[sessionID]
	if !ok || time.Now().After(session.ExpiresAt) {
		return false
	}

	switch role {
	case "host":
		return session.HostToken == token
	case "joiner":
		return session.JoinToken == token
	default:
		return false
	}
}

func (s *Store) cleanupLoop() {
	ticker := time.NewTicker(time.Minute)
	for range ticker.C {
		s.cleanup()
	}
}

func (s *Store) cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for id, session := range s.sessions {
		if now.After(session.ExpiresAt) {
			delete(s.byCodes, session.Code)
			delete(s.sessions, id)
		}
	}
}

// generateID generates a random hex ID
func generateID(length int) (string, error) {
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", b), nil
}

// generateCode generates a 12-char base32 join code (XXXX-XXXX-XXXX)
func generateCode() (string, error) {
	b := make([]byte, 8) // 8 bytes = 64 bits, encodes to ~13 base32 chars
	if _, err := rand.Read(b); err != nil {
		return "", err
	}

	encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)
	encoded = strings.ToUpper(encoded[:12]) // Take first 12 chars

	// Format as XXXX-XXXX-XXXX
	return fmt.Sprintf("%s-%s-%s", encoded[0:4], encoded[4:8], encoded[8:12]), nil
}
