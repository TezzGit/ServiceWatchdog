package main

import (
	"errors"
	"fmt"
	"log"
	"strings"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

type Dependency struct {
	Name     string `json:"name"`
	Location string `json:"location,omitempty"`
	IP       string `json:"ip,omitempty"`
}

// Unique Dependencies
type DependencyKey struct {
	Name     string
	Location string
	IP       string
}

func (d Dependency) Key() DependencyKey {
	return DependencyKey(d)
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

func (d *Dependency) queryState() (svc.State, error) {
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

func (d *Dependency) HealthCheck() (bool, error) {
	dependencyState, err := d.queryState()

	if err != nil {
		return false, fmt.Errorf("failed to connect to service manager: %w", err)
	}

	switch dependencyState {
	case svc.Stopped, svc.StopPending:
		log.Printf("%v is stopping", d.Name)
	case svc.Paused, svc.PausePending:
		log.Printf("%v is pausing", d.Name)
	default:
		log.Printf("%v's current state: %v", d.Name, dependencyState)
		return true, nil
	}
	return false, nil
}
