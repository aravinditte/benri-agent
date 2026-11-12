// Package auth implements password verification (scrypt, as in v2) and
// session-token handling. Tokens are stored SHA-256-hashed at rest.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"strings"

	"golang.org/x/crypto/scrypt"
)

func HashPassword(password string) string {
	salt := make([]byte, 16)
	_, _ = rand.Read(salt)
	h, _ := scrypt.Key([]byte(password), salt, 16384, 8, 1, 32)
	return fmt.Sprintf("scrypt:%x:%x", salt, h)
}

func VerifyPassword(password, stored string) bool {
	parts := strings.Split(stored, ":")
	if len(parts) != 3 || parts[0] != "scrypt" {
		return false
	}
	salt, err1 := hex.DecodeString(parts[1])
	expected, err2 := hex.DecodeString(parts[2])
	if err1 != nil || err2 != nil {
		return false
	}
	actual, err := scrypt.Key([]byte(password), salt, 16384, 8, 1, 32)
	if err != nil || len(actual) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare(actual, expected) == 1
}

// HashToken is also the generic content hash used for drafts/payloads in v2
// (SHA-256 hex); kept identical so hash semantics do not change.
func HashToken(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func ContentHash(s string) string { return HashToken(s) }

func NewSessionToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
