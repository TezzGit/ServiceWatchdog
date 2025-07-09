package main

type serviceWatcher struct {
	Email    EmailConfig `json:"email"`
	Services []Service   `json:"services"`
}

type EmailConfig struct {
	From       string `json:"from"`
	To         string `json:"to"`
	Subject    string `json:"subject"`
	SMTPServer string `json:"smtp_server"`
	SMTPPort   int    `json:"smtp_port"`
	UseTLS     bool   `json:"use_tls"`
}

const TCP_TIMEOUT = 5
const MAX_CONCURRENT = 10
