package main

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

// Dependencies Not Required
type Service struct {
	Name             string         `json:"name"`
	Dependencies     []Dependency   `json:"dependencies,omitempty"`
	RecoverySequence []RecoveryStep `json:"recovery_sequence"`
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

func (s *Service) HealthCheck(maxDepConcurrency int, cache *HealthCache) ([]HealthStatus, error) {
	scm, err := mgr.Connect()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to local SCM: %w", err)
	}
	defer scm.Disconnect()

	svcState, err := s.QueryState(scm)
	if err != nil {
		return nil, err
	}

	results := []HealthStatus{{
		Name:      s.Name,
		IsService: true,
		Healthy:   isHealthyState(s.Name, svcState),
		Err:       nil,
	}}

	if !results[0].Healthy {
		sem := make(chan struct{}, maxDepConcurrency)
		var wg sync.WaitGroup
		resultsCh := make(chan HealthStatus, len(s.Dependencies))

		for _, dep := range s.Dependencies {
			dep := dep
			wg.Add(1)
			go func() {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				status := cache.GetOrRun(dep, dep.HealthCheck)
				resultsCh <- status
			}()
		}
		wg.Wait()
		close(resultsCh)

		for r := range resultsCh {
			results = append(results, r)
		}
	}

	return results, nil
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
