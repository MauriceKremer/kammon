package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"crypto/pbkdf2"
)

// DefaultPBKDF2Iterations is the iteration count for new PBKDF2-SHA256 hashes (MON-WA-4).
// 100,000 iterations is OWASP recommended for PBKDF2-HMAC-SHA256 while keeping verification
// fast on low-power devices (~10-25ms on small ARM boards).
const DefaultPBKDF2Iterations = 100000

// HashPassword hashes a password using PBKDF2-HMAC-SHA256 with a 16-byte random salt (MON-WA-4).
// Format: $pbkdf2-sha256$i=100000$<salt_hex>$<hash_hex>
func HashPassword(password string) (string, error) {
	if password == "" {
		return "", errors.New("password cannot be empty")
	}
	var salt [16]byte
	if _, err := rand.Read(salt[:]); err != nil {
		return "", fmt.Errorf("read random salt: %w", err)
	}
	derived, err := pbkdf2.Key(sha256.New, password, salt[:], DefaultPBKDF2Iterations, 32)
	if err != nil {
		return "", fmt.Errorf("derive key: %w", err)
	}
	return fmt.Sprintf("$pbkdf2-sha256$i=%d$%s$%s", DefaultPBKDF2Iterations, hex.EncodeToString(salt[:]), hex.EncodeToString(derived)), nil
}

// VerifyPassword verifies password against stored. Stored can be:
// 1. A formatted PBKDF2 hash: $pbkdf2-sha256$i=<iter>$<salt>$<hash> (MON-WA-4)
// 2. A legacy plain-text password for backward compatibility (MON-WA-3)
func VerifyPassword(password, stored string) bool {
	if strings.HasPrefix(stored, "$pbkdf2-sha256$") {
		return verifyPBKDF2(password, stored)
	}
	// Fall back to legacy plain-text password check (constant time, MON-AX-5).
	return subtle.ConstantTimeCompare([]byte(stored), []byte(password)) == 1
}

func verifyPBKDF2(password, stored string) bool {
	parts := strings.Split(stored, "$")
	// Format is "$pbkdf2-sha256$i=100000$<salt>$<hash>" -> 5 parts (first part is empty).
	if len(parts) != 5 || parts[1] != "pbkdf2-sha256" {
		return false
	}
	iterStr := strings.TrimPrefix(parts[2], "i=")
	iter, err := strconv.Atoi(iterStr)
	// Bound check: OWASP recommends >=100k; allow 1k-5M for drift in loaded configs or future hardness.
	if err != nil || iter < 1000 || iter > 5000000 {
		return false
	}
	salt, err := hex.DecodeString(parts[3])
	if err != nil || len(salt) == 0 {
		return false
	}
	expectedHash, err := hex.DecodeString(parts[4])
	if err != nil || len(expectedHash) == 0 {
		return false
	}

	derived, err := pbkdf2.Key(sha256.New, password, salt, iter, len(expectedHash))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(derived, expectedHash) == 1
}
