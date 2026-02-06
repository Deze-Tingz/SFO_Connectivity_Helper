package p2p

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// Session holds WebRTC signaling data
type Session struct {
	ID           string
	Code         string
	HostSignals  []*SignalMessage
	JoinSignals  []*SignalMessage
	HostIndex    int
	JoinIndex    int
	CreatedAt    time.Time
	mu           sync.Mutex
}

// SignalServer handles WebRTC signaling
type SignalServer struct {
	sessions map[string]*Session
	byCodes  map[string]string
	mu       sync.RWMutex
	ttl      time.Duration
}

// NewSignalServer creates a new signaling server
func NewSignalServer(ttl time.Duration) *SignalServer {
	s := &SignalServer{
		sessions: make(map[string]*Session),
		byCodes:  make(map[string]string),
		ttl:      ttl,
	}
	go s.cleanupLoop()
	return s
}

// RunSignalServer starts the HTTP signaling server
func RunSignalServer(ctx context.Context, port int) error {
	server := NewSignalServer(15 * time.Minute)

	mux := http.NewServeMux()

	// Health check
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	// Create session
	mux.HandleFunc("/session/create", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		session, err := server.CreateSession()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"sessionId": session.ID,
			"code":      session.Code,
		})
	})

	// Join session
	mux.HandleFunc("/session/join", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			Code string `json:"code"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}

		session, err := server.JoinSession(req.Code)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"sessionId": session.ID,
		})
	})

	// Send signal
	mux.HandleFunc("/signal/send", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			SessionID string         `json:"sessionId"`
			Role      string         `json:"role"`
			Signal    *SignalMessage `json:"signal"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}

		if err := server.AddSignal(req.SessionID, req.Role, req.Signal); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		w.WriteHeader(http.StatusOK)
	})

	// Receive signal (long-polling)
	mux.HandleFunc("/signal/receive", func(w http.ResponseWriter, r *http.Request) {
		sessionID := r.URL.Query().Get("sessionId")
		role := r.URL.Query().Get("role")

		signal, err := server.GetSignal(sessionID, role)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if signal == nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"signal": signal,
		})
	})

	// CORS handler
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		mux.ServeHTTP(w, r)
	})

	httpServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: handler,
	}

	go func() {
		<-ctx.Done()
		httpServer.Shutdown(context.Background())
	}()

	fmt.Printf("[SignalServer] Listening on port %d\n", port)
	return httpServer.ListenAndServe()
}

// CreateSession creates a new signaling session
func (s *SignalServer) CreateSession() (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	id := generateID()
	code := generateCode()

	session := &Session{
		ID:          id,
		Code:        code,
		HostSignals: make([]*SignalMessage, 0),
		JoinSignals: make([]*SignalMessage, 0),
		CreatedAt:   time.Now(),
	}

	s.sessions[id] = session
	s.byCodes[code] = id

	return session, nil
}

// JoinSession finds a session by code
func (s *SignalServer) JoinSession(code string) (*Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	id, ok := s.byCodes[code]
	if !ok {
		return nil, fmt.Errorf("session not found")
	}

	session, ok := s.sessions[id]
	if !ok {
		return nil, fmt.Errorf("session expired")
	}

	return session, nil
}

// AddSignal adds a signal message to a session
func (s *SignalServer) AddSignal(sessionID, role string, signal *SignalMessage) error {
	s.mu.RLock()
	session, ok := s.sessions[sessionID]
	s.mu.RUnlock()

	if !ok {
		return fmt.Errorf("session not found")
	}

	session.mu.Lock()
	defer session.mu.Unlock()

	if role == "host" {
		session.HostSignals = append(session.HostSignals, signal)
	} else {
		session.JoinSignals = append(session.JoinSignals, signal)
	}

	return nil
}

// GetSignal retrieves the next signal for a role
func (s *SignalServer) GetSignal(sessionID, role string) (*SignalMessage, error) {
	s.mu.RLock()
	session, ok := s.sessions[sessionID]
	s.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("session not found")
	}

	session.mu.Lock()
	defer session.mu.Unlock()

	// Host receives joiner's signals, joiner receives host's signals
	if role == "host" {
		if session.HostIndex < len(session.JoinSignals) {
			signal := session.JoinSignals[session.HostIndex]
			session.HostIndex++
			return signal, nil
		}
	} else {
		if session.JoinIndex < len(session.HostSignals) {
			signal := session.HostSignals[session.JoinIndex]
			session.JoinIndex++
			return signal, nil
		}
	}

	return nil, nil
}

func (s *SignalServer) cleanupLoop() {
	ticker := time.NewTicker(time.Minute)
	for range ticker.C {
		s.cleanup()
	}
}

func (s *SignalServer) cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for id, session := range s.sessions {
		if now.Sub(session.CreatedAt) > s.ttl {
			delete(s.byCodes, session.Code)
			delete(s.sessions, id)
		}
	}
}

func generateID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}

func generateCode() string {
	const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, 4)
	rand.Read(b)
	code := make([]byte, 4)
	for i := 0; i < 4; i++ {
		code[i] = chars[int(b[i])%len(chars)]
	}
	return fmt.Sprintf("SFO-%s", string(code))
}
