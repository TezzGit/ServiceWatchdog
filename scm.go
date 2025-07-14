package main

import (
	"errors"
	"fmt"
	"log"
	"net"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

const RPC_PORT = "135"

type ServiceManager interface {
	Restart(serviceName string, attempts int, delaySeconds int) error
	Start(serviceName string) error
	Stop(serviceName string) error
	Query(serviceName string) (svc.State, error)
	Close() error
}

type LocalServiceManager struct {
	mgr *mgr.Mgr
}

type RemoteServiceManager struct {
	Host string
	IP   string
	mgr  *mgr.Mgr
}

func NewLocalServiceManager() (*LocalServiceManager, error) {
	m, err := mgr.Connect()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to local SCM: %w", err)
	}
	return &LocalServiceManager{mgr: m}, nil
}

func (sm *LocalServiceManager) Restart(serviceName string, attempts int, delay int) error {
	svcHandle, err := sm.mgr.OpenService(serviceName)
	if err != nil {
		return err
	}
	defer svcHandle.Close()

	for i := 0; i < attempts; i++ {

		_, err = svcHandle.Control(svc.Stop)
		if err != nil {
			return fmt.Errorf("failed to stop %q: %w", serviceName, err)
		}
		time.Sleep(time.Duration(delay) * time.Second)

		err = svcHandle.Start()
		if err == nil {
			return nil
		}
	}
	return fmt.Errorf("failed to restart %q after %d attempts", serviceName, attempts)
}

func (sm *LocalServiceManager) Start(serviceName string) error {
	svcHandle, err := sm.mgr.OpenService(serviceName)
	if err != nil {
		return err
	}
	defer svcHandle.Close()
	return svcHandle.Start()
}

func (sm *LocalServiceManager) Stop(serviceName string) error {
	svcHandle, err := sm.mgr.OpenService(serviceName)
	if err != nil {
		return err
	}
	defer svcHandle.Close()
	_, err = svcHandle.Control(svc.Stop)
	return err
}

func (sm *LocalServiceManager) Query(serviceName string) (svc.State, error) {
	svcHandle, err := sm.mgr.OpenService(serviceName)
	if err != nil {
		return svc.State(0), err
	}
	defer svcHandle.Close()

	status, err := svcHandle.Query()
	if err != nil {
		return svc.State(0), err
	}
	return status.State, nil
}

func (sm *LocalServiceManager) Close() error {
	return sm.mgr.Disconnect()
}

func NewRemoteServiceManager(hostname, ip string) (*RemoteServiceManager, error) {
	scm, err := connectToRemoteSCM(hostname, ip)
	if err != nil {
		return nil, err
	}
	return &RemoteServiceManager{mgr: scm}, nil
}

func (r *RemoteServiceManager) Query(serviceName string) (svc.State, error) {
	if r.mgr == nil {
		return 0, fmt.Errorf("service manager is not initialized")
	}

	svcHandle, err := r.mgr.OpenService(serviceName)
	if err != nil {
		if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
			return 0, fmt.Errorf("service %q does not exist", serviceName)
		}
		return 0, fmt.Errorf("failed to open service %q: %w", serviceName, err)
	}
	defer svcHandle.Close()

	status, err := svcHandle.Query()
	if err != nil {
		return 0, fmt.Errorf("failed to query service %q: %w", serviceName, err)
	}

	return status.State, nil
}

func (r *RemoteServiceManager) Start(serviceName string) error {
	svcHandle, err := r.mgr.OpenService(serviceName)
	if err != nil {
		return fmt.Errorf("failed to open service %q: %w", serviceName, err)
	}
	defer svcHandle.Close()

	return svcHandle.Start()
}

func (r *RemoteServiceManager) Stop(serviceName string) error {
	svcHandle, err := r.mgr.OpenService(serviceName)
	if err != nil {
		return fmt.Errorf("failed to open service %q: %w", serviceName, err)
	}
	defer svcHandle.Close()
	_, err = svcHandle.Control(svc.Stop)
	return err
}

func (r *RemoteServiceManager) Restart(serviceName string, attempts int, delaySeconds int) error {
	svcHandle, err := r.mgr.OpenService(serviceName)
	if err != nil {
		return err
	}
	defer svcHandle.Close()

	for i := 0; i < attempts; i++ {

		_, err = svcHandle.Control(svc.Stop)
		if err != nil {
			return fmt.Errorf("failed to stop %q: %w", serviceName, err)
		}
		time.Sleep(time.Duration(delaySeconds) * time.Second)

		err = svcHandle.Start()
		if err == nil {
			return nil
		}
	}
	return fmt.Errorf("failed to restart %q after %d attempts", serviceName, attempts)
}

func (r *RemoteServiceManager) Close() error {
	return r.mgr.Disconnect()
}

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
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(target, RPC_PORT), timeout)
	if err != nil {
		return err
	}
	_ = conn.Close()

	return nil
}
