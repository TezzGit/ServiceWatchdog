package main

import (
	"errors"
	"fmt"
	"strings"
	"sync"
)

// validateConfig concurrently validates all services in the config
func validateConfig(watcher *serviceWatcher, maxConcurrent int) error {

	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup

	// TODO: improve buffer - account for all potential dependencies aswell.
	errCh := make(chan error, len(watcher.Services))

	// 🔹 Step 1: Validate unique dependencies
	uniqueDeps := collectUniqueDependencies(watcher.Services)

	for key, dep := range uniqueDeps {
		wg.Add(1)
		go func(dep Dependency, key DependencyKey) {
			defer wg.Done()

			sem <- struct{}{}
			defer func() { <-sem }()

			if err := dep.Validate(); err != nil {
				errCh <- fmt.Errorf("dependency %q validation failed: %w", key.Name, err)
			}
		}(dep, key)
	}

	// 🔹 Step 2: Validate services (without validating dependencies again)
	for _, svc := range watcher.Services {
		wg.Add(1)
		go func(s Service) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			if err := s.validate(); err != nil {
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
		var b strings.Builder
		b.WriteString("validation errors:\n")
		for _, err := range errs {
			b.WriteString("- ")
			b.WriteString(err.Error())
			b.WriteString("\n")
		}
		// Print user-friendly error but still join internally
		return fmt.Errorf("%s\n(details: %w)", b.String(), errors.Join(errs...))
	}

	return nil
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
