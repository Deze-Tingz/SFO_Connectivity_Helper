package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/Deze-Tingz/SFO_Connectivity_Helper/server/internal/auth"
	"github.com/Deze-Tingz/SFO_Connectivity_Helper/server/internal/relay"
)

func main() {
	port := getEnvInt("RELAY_PORT", 8443)
	secret := getEnv("SIGNALING_SECRET", "changeme-in-production")
	signalingURL := getEnv("SIGNALING_URL", "http://localhost:8080")
	maxSessionHours := getEnvInt("MAX_SESSION_HOURS", 4)
	useTLS := getEnvBool("RELAY_TLS", false)
	certFile := getEnv("TLS_CERT_FILE", "")
	keyFile := getEnv("TLS_KEY_FILE", "")

	if secret == "changeme-in-production" {
		log.Println("WARNING: Using default secret. Set SIGNALING_SECRET in production!")
	}

	validator := &tokenValidator{
		signer:       auth.NewSigner(secret),
		signalingURL: signalingURL,
	}

	r := relay.NewRelay(
		validator,
		30*time.Second,                              // pair timeout
		time.Duration(maxSessionHours)*time.Hour,   // max session duration
	)

	addr := fmt.Sprintf(":%d", port)
	log.Printf("Relay server starting on %s (TLS: %v)", addr, useTLS)

	if useTLS && certFile != "" && keyFile != "" {
		startTLSServer(addr, certFile, keyFile, r)
	} else {
		startTCPServer(addr, r)
	}
}

func startTCPServer(addr string, r *relay.Relay) {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("Failed to start listener: %v", err)
	}
	defer listener.Close()

	log.Printf("Listening on %s", addr)
	acceptLoop(listener, r)
}

func startTLSServer(addr, certFile, keyFile string, r *relay.Relay) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		log.Fatalf("Failed to load TLS certificates: %v", err)
	}

	config := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13,
	}

	listener, err := tls.Listen("tcp", addr, config)
	if err != nil {
		log.Fatalf("Failed to start TLS listener: %v", err)
	}
	defer listener.Close()

	log.Printf("Listening on %s with TLS", addr)
	acceptLoop(listener, r)
}

func acceptLoop(listener net.Listener, r *relay.Relay) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Accept error: %v", err)
			continue
		}

		log.Printf("New connection from %s", conn.RemoteAddr())
		go r.HandleConnection(conn)
	}
}

// tokenValidator validates tokens using the local signer
type tokenValidator struct {
	signer       *auth.Signer
	signalingURL string
}

func (v *tokenValidator) Validate(token string) (sessionID, role string, err error) {
	// First try local validation (faster)
	claims, err := v.signer.Verify(token)
	if err == nil {
		return claims.SessionID, claims.Role, nil
	}

	// Fall back to signaling server validation
	return v.validateRemote(token)
}

func (v *tokenValidator) validateRemote(token string) (sessionID, role string, err error) {
	reqBody, _ := json.Marshal(map[string]string{"token": token})
	resp, err := http.Post(v.signalingURL+"/internal/validate", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		return "", "", fmt.Errorf("failed to contact signaling server: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("token validation failed: status %d", resp.StatusCode)
	}

	var result struct {
		SessionID string `json:"sessionId"`
		Role      string `json:"role"`
		Valid     bool   `json:"valid"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", fmt.Errorf("failed to decode response: %w", err)
	}

	if !result.Valid {
		return "", "", fmt.Errorf("token invalid")
	}

	return result.SessionID, result.Role, nil
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func getEnvInt(key string, defaultVal int) int {
	if val := os.Getenv(key); val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			return i
		}
	}
	return defaultVal
}

func getEnvBool(key string, defaultVal bool) bool {
	if val := os.Getenv(key); val != "" {
		return val == "true" || val == "1" || val == "yes"
	}
	return defaultVal
}
