package main

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// Dependencies Not Required
type Service struct {
	Name             string         `json:"name"`
	Dependencies     []Dependency   `json:"dependencies,omitempty"`
	RecoverySequence []RecoveryStep `json:"recovery_sequence"`

	RetryDelaySeconds int `json:"retry_delay_seconds,omitempty"`
	MaxHealthQueries  int `json:"max_retries,omitempty"`

	lastUnhealthyAt time.Time `json:"-"`
	retryCount      int       `json:"-"`
}

func (s *Service) HealthCheck(maxDepConcurrency int, cache *healthCache) ([]healthStatus, error) {
	manager, err := NewLocalServiceManager()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to local SCM: %w", err)
	}
	defer manager.Close()

	svcState, err := manager.Query(s.Name)
	if err != nil {
		return nil, err
	}

	results := []healthStatus{{
		Name:    s.Name,
		Type:    "service",
		Running: isHealthyState(s.Name, svcState),
		Err:     nil,
	}}

	if !results[0].Running {
		sem := make(chan struct{}, maxDepConcurrency)
		var wg sync.WaitGroup
		resultsCh := make(chan healthStatus, len(s.Dependencies))

		for _, dep := range s.Dependencies {
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
func (s *Service) validate() error {
	if strings.TrimSpace(s.Name) == "" {
		return errors.New("service name cannot be empty")
	}

	manager, err := NewLocalServiceManager()
	if err != nil {
		return fmt.Errorf("failed to connect to service manager: %w", err)
	}
	defer manager.Close()

	_, err = manager.Query(s.Name)
	if err != nil {
		return err
	}
	return nil
}

func (s *Service) Recover(ctx RecoveryContext) error {
	// Run Recovery Sequence
	svcMgr, err := NewLocalServiceManager()
	if err != nil {
		return fmt.Errorf("failed to connect to service manager: %w", err)
	}
	defer svcMgr.Close()

	for _, step := range s.RecoverySequence {
		if err := step.Execute(ctx); err != nil {
			log.Printf("Recovery step %v failed: %v", step.Type, err)
			if step.getOnFailureAction() == "abort" {
				return err
			}
		}
	}
	return nil
}

func (s *Service) ReQueryDelay() time.Duration {
	if s.RetryDelaySeconds <= 0 {
		return 10 * time.Second
	}
	return time.Duration(s.RetryDelaySeconds) * time.Second
}

func (s *Service) MaxQueryCount() int {
	if s.MaxHealthQueries <= 0 {
		return 3
	}
	return s.MaxHealthQueries
}

func (s *Service) ResetHealthyTracking() {
	s.lastUnhealthyAt = time.Time{}
	s.retryCount = 0
}

func (s *Service) IncrementQueryCount(now time.Time) {
	s.lastUnhealthyAt = now
	s.retryCount++
}

func (s *Service) InsideDebounceWindow(now time.Time) bool {
	return now.Sub(s.lastUnhealthyAt) < s.ReQueryDelay()
}

func (s *Service) ExceededQueryCount() bool {
	return s.retryCount >= s.MaxQueryCount()
}

func (s *Service) CurrentQueryCount() int {
	return s.retryCount
}

func collectUniqueDependencies(services []Service) map[DependencyKey]Dependency {
	unique := make(map[DependencyKey]Dependency)
	for _, svc := range services {
		for _, dep := range svc.Dependencies {
			key := DependencyKey(dep) // ✅ idiomatic and quiets staticcheck
			if _, exists := unique[key]; !exists {
				unique[key] = dep
			}
		}
	}
	return unique
}
