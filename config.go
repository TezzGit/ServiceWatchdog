package main

import (
	"encoding/json"
	"os"
)

type EmailConfig struct {
	From       string `json:"from"`
	To         string `json:"to"`
	Password   string `json:"encrypted_pass"`
	Subject    string `json:"subject"`
	SMTPServer string `json:"smtp_server"`
	SMTPPort   int    `json:"smtp_port"`
	UseTLS     bool   `json:"use_tls"`
}

const TCP_TIMEOUT = 5
const MAX_CONCURRENT = 10

func loadConfig(path string) (*serviceWatcher, error) {
	// READ JSON
	watcher, err := readConfig(path)
	if err != nil {
		return nil, err
	}

	// VALIDATE CONFIG
	if err := validateConfig(watcher, MAX_CONCURRENT); err != nil {
		return nil, err
	}

	return watcher, nil
}

// readConfig reads and unmarshals the config from disk
func readConfig(path string) (*serviceWatcher, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var watcher serviceWatcher
	if err := json.Unmarshal(data, &watcher); err != nil {
		return nil, err
	}

	return &watcher, nil
}
