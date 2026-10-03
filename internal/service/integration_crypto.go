package service

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
)

type IntegrationCredentialCipher struct {
	aead cipher.AEAD
}

func NewIntegrationCredentialCipher(secret string) (*IntegrationCredentialCipher, error) {
	sum := sha256.Sum256([]byte("integration-hub:" + secret))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &IntegrationCredentialCipher{aead: aead}, nil
}

func (c *IntegrationCredentialCipher) Encrypt(plaintext []byte) (string, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := c.aead.Seal(nil, nonce, plaintext, nil)
	buf := append(nonce, sealed...)
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func (c *IntegrationCredentialCipher) Decrypt(value string) ([]byte, error) {
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("decode integration credential: %w", err)
	}
	if len(raw) < c.aead.NonceSize() {
		return nil, fmt.Errorf("invalid integration credential")
	}
	nonce := raw[:c.aead.NonceSize()]
	ciphertext := raw[c.aead.NonceSize():]
	plaintext, err := c.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt integration credential: %w", err)
	}
	return plaintext, nil
}
