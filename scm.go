package main

import (
	"errors"
	"fmt"
	"log"
	"net"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc/mgr"
)

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
