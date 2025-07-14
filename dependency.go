package main

import (
	"errors"
	"fmt"
	"log"
	"strings"

	"golang.org/x/sys/windows/svc"
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

	manager, err := d.resolveServiceManager()
	if err != nil {
		return fmt.Errorf("SCM connection failed: %w", err)
	}
	defer manager.Close()

	return nil
}
func (d *Dependency) HealthCheck() (bool, error) {

	manager, err := d.resolveServiceManager()
	if err != nil {
		return false, err
	}
	defer manager.Close()

	state, err := manager.Query(d.Name)
	if err != nil {
		return false, fmt.Errorf("failed to query state: %w", err)
	}

	switch state {
	case svc.Stopped, svc.StopPending:
		log.Printf("%v is stopping", d.Name)
	case svc.Paused, svc.PausePending:
		log.Printf("%v is pausing", d.Name)
	default:
		log.Printf("%v's current state: %v", d.Name, state)
		return true, nil
	}
	return false, nil
}

func (d *Dependency) resolveServiceManager() (ServiceManager, error) {
	if d.Location == "localhost" || d.Location == "" {
		return NewLocalServiceManager()
	}
	return NewRemoteServiceManager(d.Location, d.IP)
}
