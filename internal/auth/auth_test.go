package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func TestIssueAndValidate(t *testing.T) {
	m, err := NewManager(tokenHash("secret-token"), time.Hour)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	token, expiresAt, err := m.Issue()
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if time.Until(expiresAt) <= 0 {
		t.Fatal("expiry must be in the future")
	}
	if err := m.Validate(token); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

// The signing key must not be derived from the access token hash, so a
// restarted process invalidates old sessions.
func TestManagersHaveIndependentSecrets(t *testing.T) {
	hash := tokenHash("secret-token")
	first, err := NewManager(hash, time.Hour)
	if err != nil {
		t.Fatalf("first manager: %v", err)
	}
	second, err := NewManager(hash, time.Hour)
	if err != nil {
		t.Fatalf("second manager: %v", err)
	}
	token, _, err := first.Issue()
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if err := second.Validate(token); err == nil {
		t.Fatal("token signed by another process was accepted")
	}
}

func TestValidateRejectsExpiredToken(t *testing.T) {
	m, err := NewManager(tokenHash("secret-token"), time.Millisecond)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	token, _, err := m.Issue()
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	time.Sleep(1200 * time.Millisecond)
	if err := m.Validate(token); err == nil {
		t.Fatal("expired token was accepted")
	}
}

func TestValidateRejectsNoneAlgorithm(t *testing.T) {
	m, err := NewManager(tokenHash("secret-token"), time.Hour)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	claims := jwt.MapClaims{"sub": "admin", "iss": "sorter", "exp": time.Now().Add(time.Hour).Unix()}
	unsigned, err := jwt.NewWithClaims(jwt.SigningMethodNone, claims).SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("sign none: %v", err)
	}
	if err := m.Validate(unsigned); err == nil {
		t.Fatal("unsigned token was accepted")
	}
}

func TestVerityTokenIsConstantTimeCompare(t *testing.T) {
	m, err := NewManager(tokenHash("secret-token"), time.Hour)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	if !m.VerifyToken("secret-token") {
		t.Fatal("correct token rejected")
	}
	if m.VerifyToken("secret-token ") || m.VerifyToken("wrong") {
		t.Fatal("incorrect token accepted")
	}
}

func TestNewManagerRejectsMalformedHash(t *testing.T) {
	cases := []string{"", "sha256:", "sha256:zz", strings.Repeat("a", 63)}
	for _, c := range cases {
		if _, err := NewManager(c, time.Hour); err == nil {
			t.Errorf("NewManager(%q) accepted a malformed hash", c)
		}
	}
}

func TestNewManagerRejectsBadTTL(t *testing.T) {
	if _, err := NewManager(tokenHash("secret-token"), 0); err == nil {
		t.Fatal("zero ttl accepted")
	}
	if _, err := NewManager(tokenHash("secret-token"), 31*24*time.Hour); err == nil {
		t.Fatal("excessive ttl accepted")
	}
}
