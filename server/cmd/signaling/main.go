package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Deze-Tingz/SFO_Connectivity_Helper/server/internal/auth"
	"github.com/Deze-Tingz/SFO_Connectivity_Helper/server/internal/ratelimit"
	"github.com/Deze-Tingz/SFO_Connectivity_Helper/server/internal/session"
)

type Server struct {
	store     *session.Store
	signer    *auth.Signer
	limiter   *ratelimit.MultiLimiter
	tokenTTL  time.Duration
}

func main() {
	port := getEnvInt("SIGNALING_PORT", 8080)
	secret := getEnv("SIGNALING_SECRET", "changeme-in-production")
	sessionTTL := time.Duration(getEnvInt("SESSION_TTL_MINUTES", 15)) * time.Minute

	if secret == "changeme-in-production" {
		log.Println("WARNING: Using default secret. Set SIGNALING_SECRET in production!")
	}

	server := &Server{
		store:    session.NewStore(sessionTTL),
		signer:   auth.NewSigner(secret),
		limiter:  ratelimit.NewMultiLimiter(),
		tokenTTL: sessionTTL,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", server.handleHealth)
	mux.HandleFunc("/session/create", server.handleCreate)
	mux.HandleFunc("/session/join", server.handleJoin)
	mux.HandleFunc("/session/", server.handleSession)
	mux.HandleFunc("/internal/validate", server.handleValidate)

	addr := fmt.Sprintf(":%d", port)
	log.Printf("Signaling server starting on %s", addr)
	if err := http.ListenAndServe(addr, server.withCORS(mux)); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

func (s *Server) withCORS(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		h.ServeHTTP(w, r)
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// POST /session/create
func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ip := getClientIP(r)
	if !s.limiter.AllowCreate(ip) {
		http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
		return
	}

	sess, err := s.store.Create()
	if err != nil {
		log.Printf("Failed to create session: %v", err)
		http.Error(w, "Failed to create session", http.StatusInternalServerError)
		return
	}

	// Create signed relay token for host
	relayToken, err := s.signer.CreateRelayToken(sess.ID, "host", s.tokenTTL)
	if err != nil {
		log.Printf("Failed to create relay token: %v", err)
		http.Error(w, "Failed to create token", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"sessionId":  sess.ID,
		"code":       sess.Code,
		"hostToken":  sess.HostToken,
		"relayToken": relayToken,
		"expiresAt":  sess.ExpiresAt.Unix(),
	})

	log.Printf("Created session %s with code %s", sess.ID, sess.Code)
}

// POST /session/join
func (s *Server) handleJoin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ip := getClientIP(r)
	if !s.limiter.AllowJoin(ip) {
		http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
		return
	}

	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// Normalize code (uppercase, handle with or without dashes)
	code := strings.ToUpper(strings.ReplaceAll(req.Code, "-", ""))
	if len(code) == 12 {
		code = fmt.Sprintf("%s-%s-%s", code[0:4], code[4:8], code[8:12])
	}

	sess, err := s.store.Join(code)
	if err != nil {
		log.Printf("Join failed for code %s: %v", req.Code, err)
		http.Error(w, "Invalid or expired code", http.StatusNotFound)
		return
	}

	// Create signed relay token for joiner
	relayToken, err := s.signer.CreateRelayToken(sess.ID, "joiner", s.tokenTTL)
	if err != nil {
		log.Printf("Failed to create relay token: %v", err)
		http.Error(w, "Failed to create token", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"sessionId":     sess.ID,
		"joinToken":     sess.JoinToken,
		"relayToken":    relayToken,
		"hostConnected": sess.HostConnected,
	})

	log.Printf("Joiner connected to session %s", sess.ID)
}

// GET/DELETE /session/{id}
func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	// Extract session ID from path
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/session/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		http.Error(w, "Session ID required", http.StatusBadRequest)
		return
	}

	sessionID := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	switch {
	case r.Method == http.MethodGet && action == "status":
		s.handleStatus(w, sessionID)
	case r.Method == http.MethodPost && action == "connect":
		s.handleConnect(w, r, sessionID)
	case r.Method == http.MethodDelete:
		s.handleDelete(w, r, sessionID)
	default:
		http.Error(w, "Not found", http.StatusNotFound)
	}
}

// GET /session/{id}/status
func (s *Server) handleStatus(w http.ResponseWriter, sessionID string) {
	sess, ok := s.store.GetByID(sessionID)
	if !ok {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"sessionId":     sess.ID,
		"hostConnected": sess.HostConnected,
		"joinConnected": sess.JoinConnected,
		"expiresAt":     sess.ExpiresAt.Unix(),
	})
}

// POST /session/{id}/connect (called by relay to update connection status)
func (s *Server) handleConnect(w http.ResponseWriter, r *http.Request, sessionID string) {
	var req struct {
		Role      string `json:"role"`
		Connected bool   `json:"connected"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	var err error
	switch req.Role {
	case "host":
		err = s.store.SetHostConnected(sessionID, req.Connected)
	case "joiner":
		err = s.store.SetJoinConnected(sessionID, req.Connected)
	default:
		http.Error(w, "Invalid role", http.StatusBadRequest)
		return
	}

	if err != nil {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// DELETE /session/{id}
func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request, sessionID string) {
	// Verify host token
	token := r.Header.Get("Authorization")
	if token == "" {
		http.Error(w, "Authorization required", http.StatusUnauthorized)
		return
	}

	token = strings.TrimPrefix(token, "Bearer ")
	if !s.store.ValidateToken(sessionID, token, "host") {
		http.Error(w, "Invalid token", http.StatusForbidden)
		return
	}

	s.store.Delete(sessionID)
	w.WriteHeader(http.StatusOK)
	log.Printf("Deleted session %s", sessionID)
}

// POST /internal/validate (for relay to validate tokens)
func (s *Server) handleValidate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	claims, err := s.signer.Verify(req.Token)
	if err != nil {
		http.Error(w, "Invalid token", http.StatusUnauthorized)
		return
	}

	// Verify session exists
	_, ok := s.store.GetByID(claims.SessionID)
	if !ok {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"sessionId": claims.SessionID,
		"role":      claims.Role,
		"valid":     true,
	})
}

func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}

	// Check X-Real-IP header
	xri := r.Header.Get("X-Real-IP")
	if xri != "" {
		return xri
	}

	// Fall back to remote address
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
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
