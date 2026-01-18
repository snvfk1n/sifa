package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"os"
)

// generateMuteToken creates an HMAC-SHA256 token for a task ID
func generateMuteToken(taskId string) string {
	secret := os.Getenv("TOKEN")
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(taskId))
	return hex.EncodeToString(h.Sum(nil))
}

// verifyMuteToken checks if the provided token is valid for the task ID
func verifyMuteToken(taskId, token string) bool {
	expectedToken := generateMuteToken(taskId)
	return hmac.Equal([]byte(expectedToken), []byte(token))
}
