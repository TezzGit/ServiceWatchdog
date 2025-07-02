// https://dev.to/cosmic_predator/writing-a-windows-service-in-go-1d1m

package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"encoding/json"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/debug"
)

const DEBUG_MODE = true

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

type Service struct {
	Name             string         `json:"name"`
	Dependencies     []Dependency   `json:"dependencies"`
	RecoverySequence []RecoveryStep `json:"recovery_sequence"`
}

type Dependency struct {
	Name     string `json:"name"`
	Location string `json:"location"`
	IP       string `json:"ip"`
}

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
	ServiceName          string `json:"service_name"`
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

func LoadConfig(path string) (*serviceWatcher, error) {
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

func (m *serviceWatcher) Execute(args []string, r <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	const cmdsAccepted = svc.AcceptStop | svc.AcceptShutdown | svc.AcceptPauseAndContinue

	tick := time.Tick(30 * time.Second)
	status <- svc.Status{State: svc.StartPending}
	status <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}

	for {
		select {
		case <-tick:
			log.Print("Tick Handled...!")
		case c := <-r:
			switch c.Cmd {
			case svc.Interrogate:
				status <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				log.Print("Shutting service...!")
				status <- svc.Status{State: svc.StopPending}
				return false, 1
			case svc.Pause:
				status <- svc.Status{State: svc.Paused, Accepts: cmdsAccepted}
			case svc.Continue:
				status <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}
			default:
				log.Printf("Unexepcted service control request #%d", c)
			}
		}
	}
}

func runService(name string, isDebug bool, watcher *serviceWatcher) {
	if isDebug {
		err := debug.Run(name, watcher)
		if err != nil {
			log.Fatalln("Error running service in debug mode.")
		}
	} else {
		err := svc.Run(name, watcher)
		if err != nil {
			log.Fatalln("Error running service in Service Control mode.")
		}
	}
}

func main() {
	f, err := os.OpenFile("debug.log", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalln(fmt.Errorf("error opening file: %v", err))
	}
	defer f.Close()

	log.SetOutput(f)

	watcher, err := LoadConfig("config.json")
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}
	runService("serviceWatcher", DEBUG_MODE, watcher)
}
