// https://dev.to/cosmic_predator/writing-a-windows-service-in-go-1d1m

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/debug"
	"golang.org/x/sys/windows/svc/mgr"
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
				log.Printf("Unexepcted service control request %+v", c)
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

// Validate Service to Watch Exists
func (s *Service) Validate() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("failed to connect to service manager: %w", err)
	}
	defer m.Disconnect()

	openedService, err := m.OpenService(s.Name)
	if err != nil {
		if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
			return fmt.Errorf("service '%s' does not exist", s.Name)
		}
		return fmt.Errorf("failed to open service '%s': %w", s.Name, err)
	}
	defer openedService.Close()
	return nil
}

func (d *Dependency) Validate() error {
	var scm *mgr.Mgr
	var err error

	if d.Location == "localhost" {
		scm, err = mgr.Connect()
	} else {
		scm, err = connectToRemoteSCM(d.Location, d.IP)
	}
	if err != nil {
		return fmt.Errorf("failed to connect to SCM at '%s', IP('%s'): '%w'", d.Location, d.IP, err)
	}
	defer scm.Disconnect()

	serviceHandle, err := scm.OpenService(d.Name)
	if err != nil {
		if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
			return fmt.Errorf("service '%s' does not exist on '%s'", d.Name, d.Location)
		}
		return fmt.Errorf("failed to open service '%s' on '%s': '%w'", d.Name, d.Location, err)
	}
	defer serviceHandle.Close()

	return nil
}

// Validate Dependency
func connectToRemoteSCM(hostname, ip string) (*mgr.Mgr, error) {
	if err := verifyHostnameIPMapping(hostname, ip); err != nil {
		log.Fatalln("Warning: hostname '%s' does not resolve to IP '%s': '%v'", hostname, ip, err)
	}

	// TRY Hostname
	if err := testTCPConnectivity(hostname); err == nil {
		if scm, er := tryConnect(hostname); err == nil {
			return scm, nil
		} else {
			log.Printf("Failed to connect to SCM via hostname '%s': '%v'", hostname, err)
		}
	} else {
		log.Printf("TCP connnectivity to hostname '%s' failed: '%v'", hostname, err)
	}

	// Try IP
	if err := testTCPConnectivity(ip); err == nil {
		if scm, err := tryConnectSCM(ip); err == nil {
			return scm, nil
		} else {
			log.Printf("Failed to connect to SCM via IP '%s': '%v'", ip, err)
		}
	} else {
		log.Printf("TCP connectivity to IP '%s' failed: '%v'", ip, err)
	}
	return nil, fmt.Errorf("failed to connect to SCM using hostname '%s' and IP '%s'", hostname,)
}

func tryConnectSCM(target string) (*mgr.Mgr, error) {
	if target == "" {
		return nil, errors.New("SCM target is empty")
	}
	ptr, err := windows.UTF16PtrFromString(`\\` + target)
	if err != nil {
		return nil, fmt.Errorf("invalid SCM target '%s': '%w'" target, err)
	}
	handle, err := windows.OpenSCManager(ptr, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return nil, fmt.Errorf("OpenSCManager failed for '%s': '%w'", target, err)
	}
	return &mgr.Mgr{Handle: handle}, nil
}

func verifyHostnameIPMapping(hostname, expectedIP string) error {
	ips, err := net.LookupHost(hostname)
	if err != nil {
		return fmt.Errorf("DNS resolution failed: %w", err)
	}
	for _, resolved := range ips {
		if resolved == expectedIP {
			return nil // match found
		}
	}
	return fmt.Errorf("IP '%s' not found in DNS records for hostname '%s'", expectedIP, hostname)
}

func testTCPConnectivity(target string) error {
	timeout := 3 * time.Second
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(target, "135"), timeout)
	if err != nil {
		return err
	}
	_ = conn.Close()

	return nil
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
