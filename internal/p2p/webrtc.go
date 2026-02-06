package p2p

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/pion/webrtc/v3"
)

// Free public STUN servers (for NAT traversal / hole punching)
var defaultSTUNServers = []string{
	"stun:stun.l.google.com:19302",
	"stun:stun1.l.google.com:19302",
	"stun:stun2.l.google.com:19302",
	"stun:stun.cloudflare.com:3478",
	"stun:stun.nextcloud.com:443",
}

// Free public TURN servers (relay fallback for CGNAT-to-CGNAT)
// These are provided by Open Relay Project - may have rate limits
var defaultTURNServers = []struct {
	URL      string
	Username string
	Password string
}{
	// Open Relay Project (metered.ca) - free tier
	{"turn:openrelay.metered.ca:80", "openrelayproject", "openrelayproject"},
	{"turn:openrelay.metered.ca:443", "openrelayproject", "openrelayproject"},
	{"turn:openrelay.metered.ca:443?transport=tcp", "openrelayproject", "openrelayproject"},
	// Backup: standard port
	{"turn:openrelay.metered.ca:80?transport=tcp", "openrelayproject", "openrelayproject"},
}

// PeerConnection wraps WebRTC for game traffic
type PeerConnection struct {
	pc          *webrtc.PeerConnection
	dataChannel *webrtc.DataChannel

	// For piping data
	readBuf    chan []byte
	writeMu    sync.Mutex

	// State
	connected  bool
	connectedCh chan struct{}
	closeCh    chan struct{}
	closeOnce  sync.Once

	// Callbacks
	onConnected    func()
	onDisconnected func()
	onError        func(error)
}

// SignalMessage is exchanged between peers via signaling server
type SignalMessage struct {
	Type      string `json:"type"`      // "offer", "answer", "candidate"
	SDP       string `json:"sdp,omitempty"`
	Candidate string `json:"candidate,omitempty"`
}

// NewPeerConnection creates a new WebRTC peer connection
func NewPeerConnection(turnServer, turnUser, turnPass string) (*PeerConnection, error) {
	// Configure ICE servers
	iceServers := []webrtc.ICEServer{
		{URLs: defaultSTUNServers},
	}

	// Add TURN server if provided (relay fallback)
	if turnServer != "" {
		// Custom TURN server provided
		iceServers = append(iceServers, webrtc.ICEServer{
			URLs:       []string{turnServer},
			Username:   turnUser,
			Credential: turnPass,
		})
	} else {
		// Use free public TURN servers as fallback (for CGNAT-to-CGNAT)
		for _, turn := range defaultTURNServers {
			iceServers = append(iceServers, webrtc.ICEServer{
				URLs:       []string{turn.URL},
				Username:   turn.Username,
				Credential: turn.Password,
			})
		}
		fmt.Println("[WebRTC] Using free TURN servers (CGNAT fallback enabled)")
	}

	config := webrtc.Configuration{
		ICEServers:         iceServers,
		ICETransportPolicy: webrtc.ICETransportPolicyAll, // Try all: direct, STUN, then TURN
	}

	pc, err := webrtc.NewPeerConnection(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create peer connection: %w", err)
	}

	p := &PeerConnection{
		pc:          pc,
		readBuf:     make(chan []byte, 256),
		connectedCh: make(chan struct{}),
		closeCh:     make(chan struct{}),
	}

	// Handle connection state changes
	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		switch state {
		case webrtc.PeerConnectionStateConnected:
			p.connected = true
			close(p.connectedCh)
			if p.onConnected != nil {
				p.onConnected()
			}
		case webrtc.PeerConnectionStateDisconnected, webrtc.PeerConnectionStateFailed, webrtc.PeerConnectionStateClosed:
			p.connected = false
			if p.onDisconnected != nil {
				p.onDisconnected()
			}
		}
	})

	return p, nil
}

// CreateOffer creates an SDP offer (called by host)
func (p *PeerConnection) CreateOffer() (*SignalMessage, error) {
	// Create data channel for game traffic
	dc, err := p.pc.CreateDataChannel("game", &webrtc.DataChannelInit{
		Ordered: boolPtr(true),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create data channel: %w", err)
	}
	p.setupDataChannel(dc)

	// Create offer
	offer, err := p.pc.CreateOffer(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create offer: %w", err)
	}

	// Set local description
	if err := p.pc.SetLocalDescription(offer); err != nil {
		return nil, fmt.Errorf("failed to set local description: %w", err)
	}

	// Wait for ICE gathering to complete
	<-webrtc.GatheringCompletePromise(p.pc)

	return &SignalMessage{
		Type: "offer",
		SDP:  p.pc.LocalDescription().SDP,
	}, nil
}

// HandleOffer handles an incoming offer and creates answer (called by joiner)
func (p *PeerConnection) HandleOffer(offer *SignalMessage) (*SignalMessage, error) {
	// Set remote description
	if err := p.pc.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  offer.SDP,
	}); err != nil {
		return nil, fmt.Errorf("failed to set remote description: %w", err)
	}

	// Handle incoming data channel
	p.pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		p.setupDataChannel(dc)
	})

	// Create answer
	answer, err := p.pc.CreateAnswer(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create answer: %w", err)
	}

	// Set local description
	if err := p.pc.SetLocalDescription(answer); err != nil {
		return nil, fmt.Errorf("failed to set local description: %w", err)
	}

	// Wait for ICE gathering
	<-webrtc.GatheringCompletePromise(p.pc)

	return &SignalMessage{
		Type: "answer",
		SDP:  p.pc.LocalDescription().SDP,
	}, nil
}

// HandleAnswer handles an incoming answer (called by host after receiving answer)
func (p *PeerConnection) HandleAnswer(answer *SignalMessage) error {
	return p.pc.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  answer.SDP,
	})
}

// AddICECandidate adds a remote ICE candidate
func (p *PeerConnection) AddICECandidate(candidate string) error {
	return p.pc.AddICECandidate(webrtc.ICECandidateInit{
		Candidate: candidate,
	})
}

// OnICECandidate sets callback for new ICE candidates
func (p *PeerConnection) OnICECandidate(callback func(*SignalMessage)) {
	p.pc.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		if candidate != nil {
			callback(&SignalMessage{
				Type:      "candidate",
				Candidate: candidate.ToJSON().Candidate,
			})
		}
	})
}

func (p *PeerConnection) setupDataChannel(dc *webrtc.DataChannel) {
	p.dataChannel = dc

	dc.OnOpen(func() {
		fmt.Println("[WebRTC] Data channel opened")
	})

	dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		select {
		case p.readBuf <- msg.Data:
		default:
			// Buffer full, drop oldest
			select {
			case <-p.readBuf:
			default:
			}
			p.readBuf <- msg.Data
		}
	})

	dc.OnClose(func() {
		fmt.Println("[WebRTC] Data channel closed")
		p.Close()
	})

	dc.OnError(func(err error) {
		if p.onError != nil {
			p.onError(err)
		}
	})
}

// WaitForConnection waits until connected or timeout
func (p *PeerConnection) WaitForConnection(timeout time.Duration) error {
	select {
	case <-p.connectedCh:
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("connection timeout")
	case <-p.closeCh:
		return fmt.Errorf("connection closed")
	}
}

// Read reads data from the peer
func (p *PeerConnection) Read(b []byte) (int, error) {
	select {
	case data := <-p.readBuf:
		n := copy(b, data)
		return n, nil
	case <-p.closeCh:
		return 0, io.EOF
	}
}

// Write sends data to the peer
func (p *PeerConnection) Write(b []byte) (int, error) {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()

	if p.dataChannel == nil || p.dataChannel.ReadyState() != webrtc.DataChannelStateOpen {
		return 0, fmt.Errorf("data channel not ready")
	}

	if err := p.dataChannel.Send(b); err != nil {
		return 0, err
	}
	return len(b), nil
}

// Close closes the connection
func (p *PeerConnection) Close() error {
	p.closeOnce.Do(func() {
		close(p.closeCh)
		if p.dataChannel != nil {
			p.dataChannel.Close()
		}
		if p.pc != nil {
			p.pc.Close()
		}
	})
	return nil
}

// IsConnected returns connection state
func (p *PeerConnection) IsConnected() bool {
	return p.connected
}

// SetCallbacks sets event callbacks
func (p *PeerConnection) SetCallbacks(onConnected, onDisconnected func(), onError func(error)) {
	p.onConnected = onConnected
	p.onDisconnected = onDisconnected
	p.onError = onError
}

// GetConnectionType returns how the connection was established
func (p *PeerConnection) GetConnectionType() string {
	if p.pc == nil {
		return "none"
	}

	stats := p.pc.GetStats()
	for _, stat := range stats {
		if candidatePair, ok := stat.(webrtc.ICECandidatePairStats); ok {
			if candidatePair.State == webrtc.StatsICECandidatePairStateSucceeded {
				// Check if using relay
				for _, s := range stats {
					if local, ok := s.(webrtc.ICECandidateStats); ok {
						if local.ID == candidatePair.LocalCandidateID {
							return string(local.CandidateType)
						}
					}
				}
			}
		}
	}
	return "unknown"
}

// BridgeToTCP bridges the WebRTC data channel to a TCP connection
func (p *PeerConnection) BridgeToTCP(addr string) error {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to connect to %s: %w", addr, err)
	}
	defer conn.Close()

	var wg sync.WaitGroup
	wg.Add(2)

	// WebRTC -> TCP
	go func() {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			n, err := p.Read(buf)
			if err != nil {
				return
			}
			if _, err := conn.Write(buf[:n]); err != nil {
				return
			}
		}
	}()

	// TCP -> WebRTC
	go func() {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			n, err := conn.Read(buf)
			if err != nil {
				return
			}
			if _, err := p.Write(buf[:n]); err != nil {
				return
			}
		}
	}()

	wg.Wait()
	return nil
}

func boolPtr(b bool) *bool {
	return &b
}

// EncodeSignal encodes a signal message to JSON
func EncodeSignal(msg *SignalMessage) (string, error) {
	data, err := json.Marshal(msg)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// DecodeSignal decodes a JSON signal message
func DecodeSignal(data string) (*SignalMessage, error) {
	var msg SignalMessage
	if err := json.Unmarshal([]byte(data), &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}
