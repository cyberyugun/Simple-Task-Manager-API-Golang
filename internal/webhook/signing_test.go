package webhook

import "testing"

func TestSignatureRoundTrip(t *testing.T) {
	secret := DeriveSigningSecret("12345678901234567890123456789012", 42)
	if len(secret) != 64 {
		t.Fatalf("derived secret length = %d, want 64", len(secret))
	}
	body := []byte(`{"event":"task.created"}`)
	signature := Signature(secret, 1234567890, body)
	if !Verify(secret, 1234567890, body, signature) {
		t.Fatal("signature should verify")
	}
	if Verify(secret, 1234567891, body, signature) {
		t.Fatal("signature must bind timestamp")
	}
	if Verify(secret, 1234567890, []byte("changed"), signature) {
		t.Fatal("signature must bind payload")
	}
}
