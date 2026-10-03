package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
)

func DeriveSigningSecret(masterKey string, subscriptionID int64) string {
	mac := hmac.New(sha256.New, []byte(masterKey))
	_, _ = mac.Write([]byte("webhook-subscription:"))
	_, _ = mac.Write([]byte(strconv.FormatInt(subscriptionID, 10)))
	return hex.EncodeToString(mac.Sum(nil))
}

func Signature(secret string, timestamp int64, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(strconv.FormatInt(timestamp, 10)))
	_, _ = mac.Write([]byte("."))
	_, _ = mac.Write(body)
	return "v1=" + hex.EncodeToString(mac.Sum(nil))
}

func Verify(secret string, timestamp int64, body []byte, signature string) bool {
	expected := Signature(secret, timestamp, body)
	return hmac.Equal([]byte(expected), []byte(signature))
}

func DeliveryID(id int64) string {
	return fmt.Sprintf("whd_%d", id)
}
