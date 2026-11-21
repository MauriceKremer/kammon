package auth

import (
	"testing"
)

func TestHashAndVerifyPassword(t *testing.T) {
	hash, err := HashPassword("secret123")
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}
	if !VerifyPassword("secret123", hash) {
		t.Fatalf("VerifyPassword failed for correct password")
	}
	if VerifyPassword("wrongpassword", hash) {
		t.Fatalf("VerifyPassword succeeded for wrong password")
	}
}

func TestVerifyLegacyPlainText(t *testing.T) {
	plain := "my-plain-pass"
	if !VerifyPassword("my-plain-pass", plain) {
		t.Fatalf("VerifyPassword failed for plain text password")
	}
	if VerifyPassword("wrong", plain) {
		t.Fatalf("VerifyPassword succeeded for wrong plain text password")
	}
}

func TestHashPasswordEmpty(t *testing.T) {
	if _, err := HashPassword(""); err == nil {
		t.Fatalf("HashPassword expected error on empty password, got nil")
	}
}

func TestVerifyCorruptedHash(t *testing.T) {
	for _, bad := range []string{
		"$pbkdf2-sha256$i=notanint$salt$hash",
		"$pbkdf2-sha256$i=10$salt$hash",
		"$pbkdf2-sha256$i=100000$nothex$hash",
		"$pbkdf2-sha256$i=100000$abcd$nothex",
		"otherprefix$i=100$salt$hash",
	} {
		if VerifyPassword("pass", bad) {
			t.Errorf("VerifyPassword should fail on corrupted hash: %s", bad)
		}
	}
}
