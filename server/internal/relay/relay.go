package relay

import (
	"encoding/json"
	"io"
	"log"
	"net"
	"sync"
	"time"
)

// AuthMessage is sent by clients to authenticate with the relay
type AuthMessage struct {
	SessionID  string `json:"sessionId"`
	RelayToken string `json:"relayToken"`
	Role       string `json:"role"` // "host" or "joiner"
}

// AuthResponse is sent back to clients after authentication
type AuthResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// PendingConnection represents a client waiting to be paired
type PendingConnection struct {
	Conn      net.Conn
	Role      string
	SessionID string
	CreatedAt time.Time
}

// Relay handles pairing and forwarding between host and joiner
type Relay struct {
	mu          sync.Mutex
	pending     map[string]*PendingConnection // sessionID -> pending host or joiner
	validator   TokenValidator
	pairTimeout time.Duration
	maxDuration time.Duration
}

// TokenValidator validates relay tokens
type TokenValidator interface {
	Validate(token string) (sessionID, role string, err error)
}

// NewRelay creates a new relay instance
func NewRelay(validator TokenValidator, pairTimeout, maxDuration time.Duration) *Relay {
	r := &Relay{
		pending:     make(map[string]*PendingConnection),
		validator:   validator,
		pairTimeout: pairTimeout,
		maxDuration: maxDuration,
	}
	go r.cleanupLoop()
	return r
}

// HandleConnection processes a new client connection
func (r *Relay) HandleConnection(conn net.Conn) {
	defer conn.Close()

	// Set initial deadline for auth message
	conn.SetDeadline(time.Now().Add(10 * time.Second))

	// Read auth message
	decoder := json.NewDecoder(conn)
	var authMsg AuthMessage
	if err := decoder.Decode(&authMsg); err != nil {
		log.Printf("Failed to read auth message: %v", err)
		r.sendAuthResponse(conn, false, "Invalid auth message")
		return
	}

	// Validate token
	sessionID, role, err := r.validator.Validate(authMsg.RelayToken)
	if err != nil {
		log.Printf("Token validation failed: %v", err)
		r.sendAuthResponse(conn, false, "Invalid token")
		return
	}

	if sessionID != authMsg.SessionID || role != authMsg.Role {
		log.Printf("Token mismatch: expected %s/%s, got %s/%s", sessionID, role, authMsg.SessionID, authMsg.Role)
		r.sendAuthResponse(conn, false, "Token mismatch")
		return
	}

	log.Printf("Authenticated %s for session %s", role, sessionID)

	// Send success response
	if err := r.sendAuthResponse(conn, true, ""); err != nil {
		log.Printf("Failed to send auth response: %v", err)
		return
	}

	// Clear deadline - will be set during pairing
	conn.SetDeadline(time.Time{})

	// Try to pair with existing connection
	r.mu.Lock()
	pending, hasPending := r.pending[sessionID]

	if hasPending && pending.Role != role {
		// Found a match - pair them
		delete(r.pending, sessionID)
		r.mu.Unlock()

		var hostConn, joinerConn net.Conn
		if role == "host" {
			hostConn = conn
			joinerConn = pending.Conn
		} else {
			hostConn = pending.Conn
			joinerConn = conn
		}

		r.pairConnections(sessionID, hostConn, joinerConn)
	} else {
		// No match yet - add to pending
		if hasPending {
			// Same role trying to connect twice - close the old one
			pending.Conn.Close()
		}

		r.pending[sessionID] = &PendingConnection{
			Conn:      conn,
			Role:      role,
			SessionID: sessionID,
			CreatedAt: time.Now(),
		}
		r.mu.Unlock()

		// Wait for pair or timeout
		r.waitForPair(conn, sessionID, role)
	}
}

func (r *Relay) sendAuthResponse(conn net.Conn, success bool, errMsg string) error {
	resp := AuthResponse{Success: success, Error: errMsg}
	data, _ := json.Marshal(resp)
	data = append(data, '\n')
	_, err := conn.Write(data)
	return err
}

func (r *Relay) waitForPair(conn net.Conn, sessionID, role string) {
	deadline := time.Now().Add(r.pairTimeout)

	for {
		// Check if we've been paired (removed from pending)
		r.mu.Lock()
		pending, stillPending := r.pending[sessionID]
		if !stillPending || pending.Conn != conn {
			r.mu.Unlock()
			return // We've been paired by another goroutine
		}
		r.mu.Unlock()

		if time.Now().After(deadline) {
			log.Printf("Pair timeout for %s in session %s", role, sessionID)
			r.mu.Lock()
			if p, ok := r.pending[sessionID]; ok && p.Conn == conn {
				delete(r.pending, sessionID)
			}
			r.mu.Unlock()
			return
		}

		time.Sleep(100 * time.Millisecond)
	}
}

func (r *Relay) pairConnections(sessionID string, hostConn, joinerConn net.Conn) {
	log.Printf("Paired session %s", sessionID)

	// Set max session deadline
	deadline := time.Now().Add(r.maxDuration)
	hostConn.SetDeadline(deadline)
	joinerConn.SetDeadline(deadline)

	var wg sync.WaitGroup
	wg.Add(2)

	// Forward host -> joiner
	go func() {
		defer wg.Done()
		n, err := io.Copy(joinerConn, hostConn)
		log.Printf("Session %s: host->joiner copied %d bytes, err: %v", sessionID, n, err)
		joinerConn.Close()
	}()

	// Forward joiner -> host
	go func() {
		defer wg.Done()
		n, err := io.Copy(hostConn, joinerConn)
		log.Printf("Session %s: joiner->host copied %d bytes, err: %v", sessionID, n, err)
		hostConn.Close()
	}()

	wg.Wait()
	log.Printf("Session %s ended", sessionID)
}

func (r *Relay) cleanupLoop() {
	ticker := time.NewTicker(10 * time.Second)
	for range ticker.C {
		r.cleanup()
	}
}

func (r *Relay) cleanup() {
	r.mu.Lock()
	defer r.mu.Unlock()

	threshold := time.Now().Add(-r.pairTimeout)
	for sessionID, pending := range r.pending {
		if pending.CreatedAt.Before(threshold) {
			pending.Conn.Close()
			delete(r.pending, sessionID)
			log.Printf("Cleaned up stale pending connection for session %s", sessionID)
		}
	}
}

// Stats returns relay statistics
func (r *Relay) Stats() map[string]interface{} {
	r.mu.Lock()
	defer r.mu.Unlock()

	return map[string]interface{}{
		"pendingConnections": len(r.pending),
	}
}
