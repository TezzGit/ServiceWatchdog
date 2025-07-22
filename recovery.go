package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"net/smtp"
	"os/exec"
	"strconv"
	"time"
)

type RecoveryActionType string

const (
	RestartService RecoveryActionType = "restart_service"
	RunScript      RecoveryActionType = "run_script"
	SendEmail      RecoveryActionType = "send_email"
)

type RecoveryStep struct {
	Type           RecoveryActionType    `json:"type"`
	RestartService *RestartServiceAction `json:"restart_service,omitempty"`
	RunScript      *RunScriptAction      `json:"run_script,omitempty"`
	SendEmail      *SendEmailAction      `json:"send_email,omitempty"`
}

type RestartServiceAction struct {
	MaxAttempts          int    `json:"max_attempts"`
	DelayBetweenAttempts int    `json:"delay_between_attempts"`
	OnFailure            string `json:"on_failure"`
}

type RunScriptAction struct {
	ScriptPath     string   `json:"script_path"`
	Args           []string `json:"args"`
	TimeoutSeconds int      `json:"timeout_seconds"`
	OnFailure      string   `json:"on_failure"`
}

type SendEmailAction struct {
	OnFailure string `json:"on_failure"`
	Body      string `json:"body,omitempty"`
	Subject   string `json:"subject,omitempty"`
}

// Validate Recovery Steps
func (r *RecoveryStep) Validate() error {
	switch r.Type {
	case RestartService:
		if r.RestartService == nil {
			return fmt.Errorf("missing or invalid restart_service action")
		}
	case RunScript:
		if r.RunScript == nil || r.RunScript.ScriptPath == "" {
			return fmt.Errorf("missing run_script config")
		}
	case SendEmail:
		if r.SendEmail == nil {
			return fmt.Errorf("missing or invalid send_email action")
		}
	}

	return nil
}

func (r *RecoveryStep) getOnFailureAction() string {
	switch r.Type {
	case RestartService:
		if r.RestartService != nil {
			return r.RestartService.OnFailure
		}

	case RunScript:
		if r.RunScript != nil {
			return r.RunScript.OnFailure
		}
	case SendEmail:
		if r.SendEmail != nil {
			return r.SendEmail.OnFailure
		}
	}
	return "abort"
}

func (r *RecoveryStep) Execute(ctx RecoveryContext) error {
	switch r.Type {
	case RestartService:
		return ctx.Services.Restart(ctx.ServiceName, r.RestartService.MaxAttempts, r.RestartService.DelayBetweenAttempts)
	case RunScript:
		return r.RunScript.RunScript()
	case SendEmail:
		return r.SendEmail.SendEmail(ctx.Email, ctx.ServiceName)
	default:
		return fmt.Errorf("unknown recovery type: %s", r.Type)
	}
}

func (r *RunScriptAction) RunScript() error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(r.TimeoutSeconds)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, r.ScriptPath, r.Args...)
	output, err := cmd.CombinedOutput()

	log.Printf("Running script: %s %v", r.ScriptPath, r.Args)
	log.Printf("Output:\n%s", output)

	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("script execution timed out after %d seconds", r.TimeoutSeconds)
	}

	if err != nil {
		return fmt.Errorf("script failed: %w", err)
	}

	return nil
}

func (s *SendEmailAction) SendEmail(cfg EmailConfig, svcName string) error {
	// Decrypt password
	encBytes, err := base64.StdEncoding.DecodeString(cfg.Password)
	if err != nil {
		return fmt.Errorf("base64 decode failed: %w", err)
	}

	decBytes, err := cfg.Encryption.Decrypt(encBytes)
	if err != nil {
		return fmt.Errorf("dpapi decrypt failed: %w", err)
	}

	auth := smtp.PlainAuth("", cfg.From, string(decBytes), cfg.SMTPServer)

	// Prefer SendEmailAction's Subject but Handle for Global
	subject := s.Subject
	if subject == "" {
		subject = cfg.Subject
	}

	body := s.Body
	if body == "" {
		body = fmt.Sprintf("Urgent: %q has failed to recover and is not running", svcName)
	}

	msg := []byte(fmt.Sprintf(
		"From: %s\r\nTo: %s\r\nSubject: %s\r\n\r\n%s",
		cfg.From, cfg.To, subject, body,
	))

	addr := cfg.SMTPServer + ":" + strconv.Itoa(cfg.SMTPPort)
	if err := smtp.SendMail(addr, auth, cfg.From, []string{cfg.To}, msg); err != nil {
		return fmt.Errorf("smtp send failed: %w", err)
	}

	log.Printf("Email sent to %s", cfg.To)
	return nil
}
