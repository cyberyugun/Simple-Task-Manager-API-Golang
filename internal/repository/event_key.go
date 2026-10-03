package repository

import (
	"crypto/rand"
	"encoding/hex"
)

func randomEventKey() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return "evt_" + hex.EncodeToString(raw[:]), nil
}
