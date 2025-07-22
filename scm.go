package main

import (
	"errors"
	"fmt"
	"log"
	"net"
	"slices"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

const RPC_PORT = "135"

type ServiceManager interface {
	Restart(serviceName string, attempts int, delay int) error
	Start(serviceName string) error
	Stop(serviceName string) error
	Query(serviceName string) (svc.State, error)
	IsRunning(serviceName string) (bool, error)
	Close() error
}

type baseServiceManager struct {
	mgr *mgr.Mgr
}

type LocalServiceManager struct {
	baseServiceManager
}

type RemoteServiceManager struct {
	baseServiceManager
	Host string
	IP   string
}

func NewLocalServiceManager() (*LocalServiceManager, error) {
	m, err := mgr.Connect()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to local SCM: %w", err)
	}
	return &LocalServiceManager{baseServiceManager{mgr: m}}, nil
}

func NewRemoteServiceManager(hostname, ip string) (*RemoteServiceManager, error) {
	scm, err := connectRemoteSCM(hostname, ip)
	if err != nil {
		return nil, err
	}
	return &RemoteServiceManager{
		baseServiceManager: baseServiceManager{mgr: scm},
		Host:               hostname,
		IP:                 ip,
	}, nil
}

func (b *baseServiceManager) openService(name string) (*mgr.Service, error) {
	return b.mgr.OpenService(name)
}

func (b *baseServiceManager) Start(name string) error {
	svcHandle, err := b.openService(name)
	if err != nil {
		return err
	}
	defer svcHandle.Close()
	return svcHandle.Start()
}

func (b *baseServiceManager) Stop(name string) error {
	svcHandle, err := b.openService(name)
	if err != nil {
		return err
	}
	defer svcHandle.Close()
	_, err = svcHandle.Control(svc.Stop)
	return err
}

func (b *baseServiceManager) Query(name string) (svc.State, error) {
	svcHandle, err := b.openService(name)
	if err != nil {
		return 0, err
	}
	defer svcHandle.Close()
	status, err := svcHandle.Query()
	if err != nil {
		return 0, err
	}
	return status.State, nil
}

func (b *baseServiceManager) Restart(name string, attempts int, delay int) error {
	svcHandle, err := b.openService(name)
	if err != nil {
		return err
	}
	defer svcHandle.Close()

	for range attempts {
		_, err = svcHandle.Control(svc.Stop)
		if err != nil && !isIgnorableStopError(err) {
			return fmt.Errorf("failed to stop %q: %w", name, err)
		}
		time.Sleep(time.Duration(delay) * time.Second)
		err = svcHandle.Start()
		if err == nil {
			return nil
		}
		time.Sleep(time.Duration(delay) * time.Second)
	}
	return fmt.Errorf("failed to restart %q after %d attempts", name, attempts)
}

func (b *baseServiceManager) Close() error {
	return b.mgr.Disconnect()
}

func (b *baseServiceManager) IsRunning(name string) (bool, error) {

	state, err := b.Query(name)

	if err != nil {
		return false, err
	}

	if state == svc.Running {
		return true, nil
	}

	return false, nil
}

func connectRemoteSCM(hostname, ip string) (*mgr.Mgr, error) {
	if err := verifyHostnameIPMapping(hostname, ip); err != nil {
		log.Printf("Warning: hostname '%s' does not resolve to IP '%s': '%v'", hostname, ip, err)
	}

	// Try Hostname
	if err := testTCPConnectivity(hostname); err == nil {
		if scm, err := openRemoteSCM(hostname); err == nil {
			return scm, nil
		} else {
			log.Printf("Failed to connect to SCM via hostname '%s': '%v'", hostname, err)
		}
	} else {
		log.Printf("TCP connnectivity to hostname '%s' failed: '%v'", hostname, err)
	}

	// Try IP
	if err := testTCPConnectivity(ip); err == nil {
		if scm, err := openRemoteSCM(ip); err == nil {
			return scm, nil
		} else {
			log.Printf("Failed to connect to SCM via IP '%s': '%v'", ip, err)
		}
	} else {
		log.Printf("TCP connectivity to IP '%s' failed: '%v'", ip, err)
	}
	return nil, fmt.Errorf("failed to connect to SCM using hostname '%s' and IP '%s'", hostname, ip)
}

func openRemoteSCM(target string) (*mgr.Mgr, error) {
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
	if slices.Contains(ips, expectedIP) {
		return nil // match found
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

func isIgnorableStopError(err error) bool {
	return errors.Is(err, windows.ERROR_SERVICE_NOT_ACTIVE)
}

var _ ServiceManager = (*LocalServiceManager)(nil)
var _ ServiceManager = (*RemoteServiceManager)(nil)
