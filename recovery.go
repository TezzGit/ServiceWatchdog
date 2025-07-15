package main

import "fmt"

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
		// TODO: Execute Script Behaviour
		return fmt.Errorf("not implemented: %s", r.Type)
	case SendEmail:
		// TODO: Execute Email Sender Behaviour
		return fmt.Errorf("not implemented: %s", r.Type)
	default:
		return fmt.Errorf("unknown recovery type: %s", r.Type)
	}
}
