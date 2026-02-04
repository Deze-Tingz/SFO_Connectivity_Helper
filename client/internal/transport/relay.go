package transport

import (
	"bufio"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"time"
)

// RelayClient handles connection to the relay server
type RelayClient struct {
	addr      string
	useTLS    bool
	conn      net.Conn
	sessionID string
	role      string
}

// AuthMessage is sent to authenticate with the relay
type AuthMessage struct {
	SessionID  string `json:"sessionId"`
	RelayToken string `json:"relayToken"`
	Role       string `json:"role"`
}

// AuthResponse is received after authentication
type AuthResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// NewRelayClient creates a new relay client
func NewRelayClient(addr string, useTLS bool) *RelayClient {
	return &RelayClient{
		addr:   addr,
		useTLS: useTLS,
	}
}

// Connect connects to the relay server and authenticates
func (c *RelayClient) Connect(sessionID, relayToken, role string) error {
	c.sessionID = sessionID
	c.role = role

	var conn net.Conn
	var err error

	if c.useTLS {
		conn, err = tls.DialWithDialer(
			&net.Dialer{Timeout: 10 * time.Second},
			"tcp",
			c.addr,
			&tls.Config{
				MinVersion: tls.VersionTLS13,
			},
		)
	} else {
		conn, err = net.DialTimeout("tcp", c.addr, 10*time.Second)
	}

	if err != nil {
		return fmt.Errorf("failed to connect to relay: %w", err)
	}

	// Set deadline for auth handshake
	conn.SetDeadline(time.Now().Add(15 * time.Second))

	// Send auth message
	authMsg := AuthMessage{
		SessionID:  sessionID,
		RelayToken: relayToken,
		Role:       role,
	}
	data, _ := json.Marshal(authMsg)
	data = append(data, '\n')

	if _, err := conn.Write(data); err != nil {
		conn.Close()
		return fmt.Errorf("failed to send auth message: %w", err)
	}

	// Read auth response
	reader := bufio.NewReader(conn)
	respLine, err := reader.ReadBytes('\n')
	if err != nil {
		conn.Close()
		return fmt.Errorf("failed to read auth response: %w", err)
	}

	var authResp AuthResponse
	if err := json.Unmarshal(respLine, &authResp); err != nil {
		conn.Close()
		return fmt.Errorf("invalid auth response: %w", err)
	}

	if !authResp.Success {
		conn.Close()
		return fmt.Errorf("relay authentication failed: %s", authResp.Error)
	}

	// Clear deadline - the connection is now ready for bidirectional forwarding
	conn.SetDeadline(time.Time{})

	c.conn = conn
	return nil
}

// GetConn returns the underlying connection for forwarding
func (c *RelayClient) GetConn() net.Conn {
	return c.conn
}

// Close closes the relay connection
func (c *RelayClient) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// IsConnected returns true if connected to the relay
func (c *RelayClient) IsConnected() bool {
	return c.conn != nil
}

// RemoteAddr returns the remote address of the relay connection
func (c *RelayClient) RemoteAddr() string {
	if c.conn != nil {
		return c.conn.RemoteAddr().String()
	}
	return ""
}

// CheckRelayReachable tests if the relay server is reachable
func CheckRelayReachable(addr string, useTLS bool) error {
	var conn net.Conn
	var err error

	if useTLS {
		conn, err = tls.DialWithDialer(
			&net.Dialer{Timeout: 5 * time.Second},
			"tcp",
			addr,
			&tls.Config{
				MinVersion: tls.VersionTLS13,
			},
		)
	} else {
		conn, err = net.DialTimeout("tcp", addr, 5*time.Second)
	}

	if err != nil {
		return fmt.Errorf("relay unreachable: %w", err)
	}
	conn.Close()
	return nil
}
