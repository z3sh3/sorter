// Package auth verifies the pre-shared access token and issues/validates
// session JWTs.
package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
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
// The hash itself doubles as the JWT signing secret: changing the configured
// token therefore invalidates all previously issued sessions.
func NewManager(tokenHashConfig string, ttl time.Duration) (*Manager, error) {
	raw, err := hex.DecodeString(strings.TrimPrefix(tokenHashConfig, "sha256:"))
	if err != nil || len(raw) != sha256.Size {
		return nil, fmt.Errorf("invalid token_hash format")
	}
	return &Manager{tokenHash: raw, secret: raw, ttl: ttl}, nil
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
		"iat": time.Now().Unix(),
		"exp": expiresAt.Unix(),
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
	return signed, expiresAt, err
}

// Validate parses and validates a bearer JWT string.
func (m *Manager) Validate(tokenString string) error {
	_, err := jwt.Parse(tokenString, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return m.secret, nil
	})
	return err
}

// ErrUnauthorized is returned when credentials are missing or invalid.
var ErrUnauthorized = errors.New("unauthorized")
