package main

import (
	"fmt"
	"net/smtp"
	"os"
	"strconv"
)

type Email struct {
	To      string
	Subject string
	Body    string
}

type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
}

func loadSMTPConfig() (*SMTPConfig, error) {
	portStr := os.Getenv("SMTP_PORT")
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, fmt.Errorf("invalid SMTP_PORT: %w", err)
	}

	return &SMTPConfig{
		Host:     os.Getenv("SMTP_HOST"),
		Port:     port,
		Username: os.Getenv("SMTP_USERNAME"),
		Password: os.Getenv("SMTP_PASSWORD"),
		From:     os.Getenv("SMTP_FROM"),
	}, nil
}

func sendEmail(email Email) error {
	config, err := loadSMTPConfig()
	if err != nil {
		return fmt.Errorf("failed to load SMTP config: %w", err)
	}

	// Build plain-text email message
	message := fmt.Sprintf("From: %s\r\n"+
		"To: %s\r\n"+
		"Subject: %s\r\n"+
		"\r\n"+
		"%s", config.From, email.To, email.Subject, email.Body)

	// Set up authentication
	auth := smtp.PlainAuth("", config.Username, config.Password, config.Host)

	// Send the email
	addr := fmt.Sprintf("%s:%d", config.Host, config.Port)
	err = smtp.SendMail(addr, auth, config.From, []string{email.To}, []byte(message))
	if err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}

	fmt.Printf("Email sent successfully to %s\n", email.To)
	return nil
}
