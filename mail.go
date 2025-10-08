package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
)

type PostmarkEmail struct {
	From          string `json:"From"`
	To            string `json:"To"`
	Subject       string `json:"Subject"`
	TextBody      string `json:"TextBody,omitempty"`
	HtmlBody      string `json:"HtmlBody,omitempty"`
	MessageStream string `json:"MessageStream,omitempty"`
}

type PostmarkResponse struct {
	To          string `json:"To"`
	SubmittedAt string `json:"SubmittedAt"`
	MessageID   string `json:"MessageID"`
	ErrorCode   int    `json:"ErrorCode"`
	Message     string `json:"Message"`
}

func sendPostmarkEmail(token string, email PostmarkEmail) error {
	url := "https://api.postmarkapp.com/email"

	// Marshal email to JSON
	jsonData, err := json.Marshal(email)
	if err != nil {
		return fmt.Errorf("failed to marshal email: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Postmark-Server-Token", token)

	// Send request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Parse response
	var postmarkResp PostmarkResponse
	if err := json.NewDecoder(resp.Body).Decode(&postmarkResp); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	// Check for errors
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("postmark API error (code %d): %s", postmarkResp.ErrorCode, postmarkResp.Message)
	}

	fmt.Printf("Email sent successfully! MessageID: %s\n", postmarkResp.MessageID)
	return nil
}
