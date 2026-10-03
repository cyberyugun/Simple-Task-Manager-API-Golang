package auth

import (
	"testing"
	"time"
)

func TestVerifyTOTPRFC6238Compatible(t *testing.T) {
	const secret = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	now := time.Unix(59, 0)
	if !VerifyTOTP(secret, "287082", now) {
		t.Fatal("expected RFC6238-compatible six-digit code to verify")
	}
	if VerifyTOTP(secret, "287083", now) {
		t.Fatal("unexpected invalid code verification")
	}
}

func TestSecretCipherRoundTripAndTamperDetection(t *testing.T) {
	cipher, err := NewSecretCipher("example-key-material-for-unit-tests")
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := cipher.Encrypt("example-mfa-value")
	if err != nil {
		t.Fatal(err)
	}
	if encrypted == "example-mfa-value" {
		t.Fatal("value was not encrypted")
	}
	plaintext, err := cipher.Decrypt(encrypted)
	if err != nil {
		t.Fatal(err)
	}
	if plaintext != "example-mfa-value" {
		t.Fatalf("plaintext = %q", plaintext)
	}

	tampered := encrypted[:len(encrypted)-1] + "A"
	if _, err := cipher.Decrypt(tampered); err == nil {
		t.Fatal("tampered ciphertext should fail")
	}
}
