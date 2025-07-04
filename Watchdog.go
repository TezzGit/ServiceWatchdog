// https://dev.to/cosmic_predator/writing-a-windows-service-in-go-1d1m

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/debug"
	"golang.org/x/sys/windows/svc/mgr"
)

const DEBUG_MODE = true
const TCP_TIMEOUT = 5

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

// Dependencies Not Required
type Service struct {
	Name             string         `json:"name"`
	Dependencies     []Dependency   `json:"dependencies,omitempty"`
	RecoverySequence []RecoveryStep `json:"recovery_sequence"`
}

type Dependency struct {
	Name     string `json:"name"`
	Location string `json:"location,omitempty"`
	IP       string `json:"ip,omitempty"`
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
	watcher, err := readConfig(path)
	if err != nil {
		return nil, err
	}

	if err := validateConfig(watcher); err != nil {
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

// validateConfig concurrently validates all services in the config
func validateConfig(watcher *serviceWatcher) error {
	var wg sync.WaitGroup
	errCh := make(chan error, len(watcher.Services))

	for _, svc := range watcher.Services {
		wg.Add(1)
		go func(svc Service) {
			defer wg.Done()
			if err := validateService(svc); err != nil {
				errCh <- err
			}
		}(svc)
	}

	wg.Wait()
	close(errCh)

	var errs []error
	for err := range errCh {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return fmt.Errorf("validation errors: %v", errs)
	}

	return nil
}

func validateService(svc Service) error {
	if err := svc.Validate(); err != nil {
		return fmt.Errorf("service %q validation failed: %w", svc.Name, err)
	}

	for _, dep := range svc.Dependencies {
		if err := dep.Validate(); err != nil {
			return fmt.Errorf("dependency %q validation failed for service %q: %w", dep.Name, svc.Name, err)
		}
	}

	for _, step := range svc.RecoverySequence {
		if err := step.Validate(); err != nil {
			return fmt.Errorf("recovery step %q validation failed for service %q: %w", step.Type, svc.Name, err)
		}
	}

	return nil
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

func runWacherService(name string, isDebug bool, watcher *serviceWatcher) {
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

// Query Service States
func (s *Service) Validate() error {
	if strings.TrimSpace(s.Name) == "" {
		return errors.New("service name cannot be empty")
	}

	scm, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("failed to connect to service manager: %w", err)
	}
	defer scm.Disconnect()

	_, err = s.openService(scm)
	if err != nil {
		return err
	}
	return nil
}

// QueryState retrieves the current state of the service.
func (s *Service) QueryState(serviceManager *mgr.Mgr) (svc.State, error) {
	if serviceManager == nil {
		return svc.State(0), fmt.Errorf("service manager is nil")
	}

	serviceHandle, err := s.openService(serviceManager)
	if err != nil {
		return svc.State(0), err
	}
	defer serviceHandle.Close()

	status, err := serviceHandle.Query()
	if err != nil {
		return svc.State(0), fmt.Errorf("failed to query service %q: %w", s.Name, err)
	}
	return status.State, nil
}

func (s *Service) openService(mgr *mgr.Mgr) (*mgr.Service, error) {
	svcHandle, err := mgr.OpenService(s.Name)
	if err != nil {
		if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
			return nil, fmt.Errorf("service %q does not exist", s.Name)
		}
		return nil, fmt.Errorf("failed to open service %q: %w", s.Name, err)
	}
	return svcHandle, nil
}

func (d *Dependency) QueryState() (svc.State, error) {
	if strings.TrimSpace(d.Name) == "" {
		return svc.State(0), errors.New("dependency service name cannot be empty")
	}

	serviceHandle, scm, err := d.openService()
	if err != nil {
		return svc.State(0), err
	}
	defer serviceHandle.Close()
	defer scm.Disconnect()

	status, err := serviceHandle.Query()
	if err != nil {
		return svc.State(0), fmt.Errorf("failed to query service %q: %w", d.Name, err)
	}

	return status.State, nil
}

func (d *Dependency) Validate() error {
	if strings.TrimSpace(d.Name) == "" {
		return errors.New("dependency service name cannot be empty")
	}

	serviceHandle, scm, err := d.openService()
	if err != nil {
		return err
	}
	defer serviceHandle.Close()
	defer scm.Disconnect()

	return nil
}

func (d *Dependency) openService() (*mgr.Service, *mgr.Mgr, error) {
	var scm *mgr.Mgr
	var err error

	if d.Location == "localhost" {
		scm, err = mgr.Connect()
	} else {
		scm, err = connectToRemoteSCM(d.Location, d.IP)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("failed to connect to SCM at %q, IP(%q): %w", d.Location, d.IP, err)
	}

	serviceHandle, err := scm.OpenService(d.Name)
	if err != nil {
		scm.Disconnect()
		if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
			return nil, nil, fmt.Errorf("service %q does not exist on %q", d.Name, d.Location)
		}
		return nil, nil, fmt.Errorf("failed to open service %q on %q: %w", d.Name, d.Location, err)
	}
	return serviceHandle, scm, nil
}

// Validate Dependency
func connectToRemoteSCM(hostname, ip string) (*mgr.Mgr, error) {
	if err := verifyHostnameIPMapping(hostname, ip); err != nil {
		log.Printf("Warning: hostname '%s' does not resolve to IP '%s': '%v'", hostname, ip, err)
	}

	// Try Hostname
	if err := testTCPConnectivity(hostname); err == nil {
		if scm, err := tryConnectSCM(hostname); err == nil {
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
	return nil, fmt.Errorf("failed to connect to SCM using hostname '%s' and IP '%s'", hostname, ip)
}

func tryConnectSCM(target string) (*mgr.Mgr, error) {
	if target == "" {
		return nil, errors.New("SCM target is empty")
	}
	ptr, err := windows.UTF16PtrFromString(`\\` + target)
	if err != nil {
		return nil, fmt.Errorf("invalid SCM target '%s': %w", target, err)
	}
	handle, err := windows.OpenSCManager(ptr, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return nil, fmt.Errorf("OpenSCManager failed for '%s': %w", target, err)
	}
	return &mgr.Mgr{Handle: handle}, nil
}

// Validates IP matches Host Records
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

// Add Timeout Functionality
func testTCPConnectivity(target string) error {
	timeout := TCP_TIMEOUT * time.Second
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
	runWacherService("serviceWatcher", DEBUG_MODE, watcher)
}
