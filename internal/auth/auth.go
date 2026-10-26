// Package auth verifies the pre-shared access token and issues/validates
// session JWTs.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Manager verifies the pre-shared access token and issues/validates JWTs.
type Manager struct {
	tokenHash []byte // decoded sha256 of the access token
	secret    []byte // HMAC secret for JWT signing
	ttl       time.Duration
}

// NewManager builds a Manager from the configured "sha256:<hex>" token hash.
// A random per-process signing key keeps password verification and JWT signing
// independent. Restarting the process invalidates existing sessions.
func NewManager(tokenHashConfig string, ttl time.Duration) (*Manager, error) {
	raw, err := hex.DecodeString(strings.TrimPrefix(tokenHashConfig, "sha256:"))
	if err != nil || len(raw) != sha256.Size {
		return nil, fmt.Errorf("invalid token_hash format")
	}
	if ttl <= 0 || ttl > 30*24*time.Hour {
		return nil, fmt.Errorf("jwt ttl must be between 0 and 30 days")
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	return &Manager{tokenHash: raw, secret: secret, ttl: ttl}, nil
}

// VerifyToken compares the presented token against the configured hash
// in constant time.
func (m *Manager) VerifyToken(token string) bool {
	sum := sha256.Sum256([]byte(token))
	return subtle.ConstantTimeCompare(sum[:], m.tokenHash) == 1
}

// Issue creates a signed JWT for an authenticated caller.
func (m *Manager) Issue() (string, time.Time, error) {
	expiresAt := time.Now().Add(m.ttl)
	claims := jwt.MapClaims{
		"sub": "admin",
		"iss": "sorter",
		"iat": time.Now().Unix(),
		"exp": expiresAt.Unix(),
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
	return signed, expiresAt, err
}

// Validate parses and validates a bearer JWT string.
func (m *Manager) Validate(tokenString string) error {
	_, err := jwt.Parse(tokenString, func(t *jwt.Token) (any, error) {
		return m.secret, nil
	}, jwt.WithValidMethods([]string{"HS256"}), jwt.WithExpirationRequired(), jwt.WithIssuedAt(), jwt.WithIssuer("sorter"), jwt.WithSubject("admin"))
	return err
}
