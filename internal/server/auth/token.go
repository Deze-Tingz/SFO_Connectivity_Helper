package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// TokenClaims represents the claims in a signed token
type TokenClaims struct {
	SessionID string `json:"sid"`
	Role      string `json:"role"`
	ExpiresAt int64  `json:"exp"`
}

// Signer handles token signing and verification
type Signer struct {
	secret []byte
}

// NewSigner creates a new token signer with the given secret
func NewSigner(secret string) *Signer {
	return &Signer{secret: []byte(secret)}
}

// Sign creates a signed token from claims
func (s *Signer) Sign(claims *TokenClaims) (string, error) {
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("failed to marshal claims: %w", err)
	}

	mac := hmac.New(sha256.New, s.secret)
	mac.Write(payload)
	signature := mac.Sum(nil)

	token := base64.RawURLEncoding.EncodeToString(payload) + "." +
		base64.RawURLEncoding.EncodeToString(signature)

	return token, nil
}

// Verify verifies a token and returns its claims
func (s *Signer) Verify(token string) (*TokenClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid token format")
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("invalid token payload: %w", err)
	}

	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("invalid token signature: %w", err)
	}

	mac := hmac.New(sha256.New, s.secret)
	mac.Write(payload)
	expectedSig := mac.Sum(nil)

	if !hmac.Equal(signature, expectedSig) {
		return nil, fmt.Errorf("invalid signature")
	}

	var claims TokenClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("invalid claims: %w", err)
	}

	if time.Now().Unix() > claims.ExpiresAt {
		return nil, fmt.Errorf("token expired")
	}

	return &claims, nil
}

// CreateRelayToken creates a signed token for relay authentication
func (s *Signer) CreateRelayToken(sessionID, role string, ttl time.Duration) (string, error) {
	claims := &TokenClaims{
		SessionID: sessionID,
		Role:      role,
		ExpiresAt: time.Now().Add(ttl).Unix(),
	}
	return s.Sign(claims)
}
