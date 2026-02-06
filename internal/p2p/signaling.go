package p2p

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// SignalingClient handles WebRTC signaling
type SignalingClient struct {
	serverURL string
	sessionID string
	role      string
	client    *http.Client
}

// NewSignalingClient creates a new signaling client
func NewSignalingClient(serverURL string) *SignalingClient {
	return &SignalingClient{
		serverURL: serverURL,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// CreateSession creates a new session and returns the join code
func (s *SignalingClient) CreateSession() (sessionID, joinCode string, err error) {
	resp, err := s.client.Post(s.serverURL+"/session/create", "application/json", nil)
	if err != nil {
		return "", "", fmt.Errorf("failed to create session: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		SessionID string `json:"sessionId"`
		Code      string `json:"code"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", fmt.Errorf("failed to decode response: %w", err)
	}

	s.sessionID = result.SessionID
	s.role = "host"
	return result.SessionID, result.Code, nil
}

// JoinSession joins an existing session with a code
func (s *SignalingClient) JoinSession(code string) (sessionID string, err error) {
	body, _ := json.Marshal(map[string]string{"code": code})
	resp, err := s.client.Post(s.serverURL+"/session/join", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("failed to join session: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("invalid code or session expired")
	}

	var result struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	s.sessionID = result.SessionID
	s.role = "joiner"
	return result.SessionID, nil
}

// SendSignal sends a signal message to the peer
func (s *SignalingClient) SendSignal(msg *SignalMessage) error {
	data, _ := json.Marshal(map[string]interface{}{
		"sessionId": s.sessionID,
		"role":      s.role,
		"signal":    msg,
	})

	resp, err := s.client.Post(s.serverURL+"/signal/send", "application/json", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to send signal: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("signal send failed")
	}
	return nil
}

// WaitForSignal waits for a signal from the peer
func (s *SignalingClient) WaitForSignal(timeout time.Duration) (*SignalMessage, error) {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		resp, err := s.client.Get(fmt.Sprintf("%s/signal/receive?sessionId=%s&role=%s",
			s.serverURL, s.sessionID, s.role))
		if err != nil {
			time.Sleep(500 * time.Millisecond)
			continue
		}

		if resp.StatusCode == 204 {
			resp.Body.Close()
			time.Sleep(500 * time.Millisecond)
			continue
		}

		var result struct {
			Signal *SignalMessage `json:"signal"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			resp.Body.Close()
			time.Sleep(500 * time.Millisecond)
			continue
		}
		resp.Body.Close()

		if result.Signal != nil {
			return result.Signal, nil
		}
		time.Sleep(500 * time.Millisecond)
	}

	return nil, fmt.Errorf("timeout waiting for signal")
}

// ExchangeSignals handles the full signaling exchange
func (s *SignalingClient) ExchangeSignals(pc *PeerConnection) error {
	if s.role == "host" {
		return s.exchangeAsHost(pc)
	}
	return s.exchangeAsJoiner(pc)
}

func (s *SignalingClient) exchangeAsHost(pc *PeerConnection) error {
	// Create and send offer
	offer, err := pc.CreateOffer()
	if err != nil {
		return fmt.Errorf("failed to create offer: %w", err)
	}

	if err := s.SendSignal(offer); err != nil {
		return fmt.Errorf("failed to send offer: %w", err)
	}

	fmt.Println("[Signaling] Offer sent, waiting for answer...")

	// Wait for answer
	answer, err := s.WaitForSignal(2 * time.Minute)
	if err != nil {
		return fmt.Errorf("failed to receive answer: %w", err)
	}

	if answer.Type != "answer" {
		return fmt.Errorf("expected answer, got %s", answer.Type)
	}

	if err := pc.HandleAnswer(answer); err != nil {
		return fmt.Errorf("failed to handle answer: %w", err)
	}

	fmt.Println("[Signaling] Answer received, connecting...")
	return nil
}

func (s *SignalingClient) exchangeAsJoiner(pc *PeerConnection) error {
	// Wait for offer
	fmt.Println("[Signaling] Waiting for offer from host...")
	offer, err := s.WaitForSignal(2 * time.Minute)
	if err != nil {
		return fmt.Errorf("failed to receive offer: %w", err)
	}

	if offer.Type != "offer" {
		return fmt.Errorf("expected offer, got %s", offer.Type)
	}

	// Create and send answer
	answer, err := pc.HandleOffer(offer)
	if err != nil {
		return fmt.Errorf("failed to create answer: %w", err)
	}

	if err := s.SendSignal(answer); err != nil {
		return fmt.Errorf("failed to send answer: %w", err)
	}

	fmt.Println("[Signaling] Answer sent, connecting...")
	return nil
}
