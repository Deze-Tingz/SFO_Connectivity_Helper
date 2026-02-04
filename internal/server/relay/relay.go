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
	Role       string `json:"role"`
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

// TokenValidator validates relay tokens
type TokenValidator interface {
	Validate(token string) (sessionID, role string, err error)
}

// Relay handles pairing and forwarding between host and joiner
type Relay struct {
	mu          sync.Mutex
	pending     map[string]*PendingConnection
	validator   TokenValidator
	pairTimeout time.Duration
	maxDuration time.Duration
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

	conn.SetDeadline(time.Now().Add(10 * time.Second))

	decoder := json.NewDecoder(conn)
	var authMsg AuthMessage
	if err := decoder.Decode(&authMsg); err != nil {
		log.Printf("Failed to read auth message: %v", err)
		r.sendAuthResponse(conn, false, "Invalid auth message")
		return
	}

	sessionID, role, err := r.validator.Validate(authMsg.RelayToken)
	if err != nil {
		log.Printf("Token validation failed: %v", err)
		r.sendAuthResponse(conn, false, "Invalid token")
		return
	}

	if sessionID != authMsg.SessionID || role != authMsg.Role {
		log.Printf("Token mismatch")
		r.sendAuthResponse(conn, false, "Token mismatch")
		return
	}

	log.Printf("Authenticated %s for session %s", role, sessionID)

	if err := r.sendAuthResponse(conn, true, ""); err != nil {
		log.Printf("Failed to send auth response: %v", err)
		return
	}

	conn.SetDeadline(time.Time{})

	r.mu.Lock()
	pending, hasPending := r.pending[sessionID]

	if hasPending && pending.Role != role {
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
		if hasPending {
			pending.Conn.Close()
		}

		r.pending[sessionID] = &PendingConnection{
			Conn:      conn,
			Role:      role,
			SessionID: sessionID,
			CreatedAt: time.Now(),
		}
		r.mu.Unlock()

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
		r.mu.Lock()
		pending, stillPending := r.pending[sessionID]
		if !stillPending || pending.Conn != conn {
			r.mu.Unlock()
			return
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

	deadline := time.Now().Add(r.maxDuration)
	hostConn.SetDeadline(deadline)
	joinerConn.SetDeadline(deadline)

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		io.Copy(joinerConn, hostConn)
		joinerConn.Close()
	}()

	go func() {
		defer wg.Done()
		io.Copy(hostConn, joinerConn)
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
		}
	}
}
