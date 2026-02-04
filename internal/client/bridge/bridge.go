package bridge

import (
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// State represents the current state of the bridge
type State int

const (
	StateInit State = iota
	StateWaitingForGame
	StateReady
	StateConnectingRelay
	StateConnected
	StateDisconnected
	StateError
)

func (s State) String() string {
	switch s {
	case StateInit:
		return "INIT"
	case StateWaitingForGame:
		return "WAITING_FOR_GAME"
	case StateReady:
		return "READY"
	case StateConnectingRelay:
		return "CONNECTING_RELAY"
	case StateConnected:
		return "CONNECTED"
	case StateDisconnected:
		return "DISCONNECTED"
	case StateError:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

// Stats tracks connection statistics
type Stats struct {
	BytesIn   atomic.Int64
	BytesOut  atomic.Int64
	StartTime time.Time
	LastError string
}

// Bridge handles the local game <-> relay connection
type Bridge struct {
	mu            sync.RWMutex
	state         State
	targetAddr    string
	relayConn     net.Conn
	localConn     net.Conn
	stats         *Stats
	onStateChange func(State)
	stopCh        chan struct{}
	wg            sync.WaitGroup
}

// NewBridge creates a new bridge instance
func NewBridge(targetAddr string) *Bridge {
	return &Bridge{
		state:      StateInit,
		targetAddr: targetAddr,
		stats:      &Stats{StartTime: time.Now()},
		stopCh:     make(chan struct{}),
	}
}

// SetStateChangeCallback sets a callback for state changes
func (b *Bridge) SetStateChangeCallback(cb func(State)) {
	b.mu.Lock()
	b.onStateChange = cb
	b.mu.Unlock()
}

// GetState returns the current state
func (b *Bridge) GetState() State {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.state
}

// GetStats returns current statistics
func (b *Bridge) GetStats() *Stats {
	return b.stats
}

func (b *Bridge) setState(state State) {
	b.mu.Lock()
	oldState := b.state
	b.state = state
	cb := b.onStateChange
	b.mu.Unlock()

	if oldState != state && cb != nil {
		cb(state)
	}
}

// WaitForGame probes the target port until the game is listening
func (b *Bridge) WaitForGame(timeout time.Duration) error {
	b.setState(StateWaitingForGame)

	deadline := time.Now().Add(timeout)
	retryInterval := 500 * time.Millisecond

	for {
		select {
		case <-b.stopCh:
			return fmt.Errorf("cancelled")
		default:
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for game on %s", b.targetAddr)
		}

		if isPortListening(b.targetAddr) {
			b.setState(StateReady)
			return nil
		}

		time.Sleep(retryInterval)
	}
}

// ConnectRelay connects to the relay and starts forwarding
func (b *Bridge) ConnectRelay(relayConn net.Conn) error {
	b.setState(StateConnectingRelay)

	localConn, err := net.DialTimeout("tcp", b.targetAddr, 5*time.Second)
	if err != nil {
		b.stats.LastError = fmt.Sprintf("failed to connect to game: %v", err)
		b.setState(StateError)
		return fmt.Errorf("failed to connect to game at %s: %w", b.targetAddr, err)
	}

	b.mu.Lock()
	b.relayConn = relayConn
	b.localConn = localConn
	b.mu.Unlock()

	b.setState(StateConnected)
	b.stats.StartTime = time.Now()

	b.wg.Add(2)
	go b.forwardToLocal()
	go b.forwardToRelay()

	return nil
}

func (b *Bridge) forwardToLocal() {
	defer b.wg.Done()

	buf := make([]byte, 32*1024)
	for {
		select {
		case <-b.stopCh:
			return
		default:
		}

		n, err := b.relayConn.Read(buf)
		if err != nil {
			if err != io.EOF {
				log.Printf("Error reading from relay: %v", err)
			}
			b.Close()
			return
		}

		if n > 0 {
			b.stats.BytesIn.Add(int64(n))
			if _, err := b.localConn.Write(buf[:n]); err != nil {
				log.Printf("Error writing to local: %v", err)
				b.Close()
				return
			}
		}
	}
}

func (b *Bridge) forwardToRelay() {
	defer b.wg.Done()

	buf := make([]byte, 32*1024)
	for {
		select {
		case <-b.stopCh:
			return
		default:
		}

		n, err := b.localConn.Read(buf)
		if err != nil {
			if err != io.EOF {
				log.Printf("Error reading from local: %v", err)
			}
			b.Close()
			return
		}

		if n > 0 {
			b.stats.BytesOut.Add(int64(n))
			if _, err := b.relayConn.Write(buf[:n]); err != nil {
				log.Printf("Error writing to relay: %v", err)
				b.Close()
				return
			}
		}
	}
}

// Close stops the bridge and closes all connections
func (b *Bridge) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	select {
	case <-b.stopCh:
		return
	default:
		close(b.stopCh)
	}

	if b.localConn != nil {
		b.localConn.Close()
	}
	if b.relayConn != nil {
		b.relayConn.Close()
	}

	b.state = StateDisconnected
}

// Wait waits for the bridge to finish
func (b *Bridge) Wait() {
	b.wg.Wait()
}

func isPortListening(addr string) bool {
	conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// CheckTargetPort checks if the target port is available/listening
func CheckTargetPort(addr string) (bool, error) {
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		return false, nil
	}
	conn.Close()
	return true, nil
}
