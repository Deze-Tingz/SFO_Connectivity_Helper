package transport

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// SignalingClient communicates with the signaling server
type SignalingClient struct {
	baseURL    string
	httpClient *http.Client
}

// CreateSessionResponse is the response from creating a session
type CreateSessionResponse struct {
	SessionID  string `json:"sessionId"`
	Code       string `json:"code"`
	HostToken  string `json:"hostToken"`
	RelayToken string `json:"relayToken"`
	ExpiresAt  int64  `json:"expiresAt"`
}

// JoinSessionResponse is the response from joining a session
type JoinSessionResponse struct {
	SessionID     string `json:"sessionId"`
	JoinToken     string `json:"joinToken"`
	RelayToken    string `json:"relayToken"`
	HostConnected bool   `json:"hostConnected"`
}

// SessionStatus is the status of a session
type SessionStatus struct {
	SessionID     string `json:"sessionId"`
	HostConnected bool   `json:"hostConnected"`
	JoinConnected bool   `json:"joinConnected"`
	ExpiresAt     int64  `json:"expiresAt"`
}

// NewSignalingClient creates a new signaling client
func NewSignalingClient(baseURL string) *SignalingClient {
	return &SignalingClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// CreateSession creates a new session
func (c *SignalingClient) CreateSession() (*CreateSessionResponse, error) {
	resp, err := c.httpClient.Post(c.baseURL+"/session/create", "application/json", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to signaling server: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("create session failed: %s (status %d)", string(body), resp.StatusCode)
	}

	var result CreateSessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// JoinSession joins an existing session with a code
func (c *SignalingClient) JoinSession(code string) (*JoinSessionResponse, error) {
	reqBody, _ := json.Marshal(map[string]string{"code": code})
	resp, err := c.httpClient.Post(c.baseURL+"/session/join", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to signaling server: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("invalid or expired join code")
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("rate limit exceeded, please wait and try again")
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("join session failed: %s (status %d)", string(body), resp.StatusCode)
	}

	var result JoinSessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// GetSessionStatus gets the current status of a session
func (c *SignalingClient) GetSessionStatus(sessionID string) (*SessionStatus, error) {
	resp, err := c.httpClient.Get(c.baseURL + "/session/" + sessionID + "/status")
	if err != nil {
		return nil, fmt.Errorf("failed to connect to signaling server: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("session not found or expired")
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get status failed: %s (status %d)", string(body), resp.StatusCode)
	}

	var result SessionStatus
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// Health checks if the signaling server is reachable
func (c *SignalingClient) Health() error {
	resp, err := c.httpClient.Get(c.baseURL + "/health")
	if err != nil {
		return fmt.Errorf("signaling server unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("signaling server unhealthy: status %d", resp.StatusCode)
	}

	return nil
}
