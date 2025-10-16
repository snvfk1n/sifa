package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"os"
)

// generateMuteToken creates an HMAC-SHA256 token for a target ID
func generateMuteToken(targetId string) string {
	secret := os.Getenv("TOKEN")
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(targetId))
	return hex.EncodeToString(h.Sum(nil))
}

// verifyMuteToken checks if the provided token is valid for the target ID
func verifyMuteToken(targetId, token string) bool {
	expectedToken := generateMuteToken(targetId)
	return hmac.Equal([]byte(expectedToken), []byte(token))
}
