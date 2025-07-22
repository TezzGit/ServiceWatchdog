package main

import (
	"Watchdog/pkg/crypto"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
)

type EmailConfig struct {
	From           string            `json:"from"`
	To             string            `json:"to"`
	Password       string            `json:"pass"`
	Subject        string            `json:"subject"`
	SMTPServer     string            `json:"smtp_server"`
	SMTPPort       int               `json:"smtp_port"`
	UseTLS         bool              `json:"use_tls"`
	EncryptionType string            `json:"encryption_type,omitempty"`
	Encryption     crypto.Encryption `json:"-"`
}

const TCP_TIMEOUT = 5
const MAX_CONCURRENT = 10

func loadConfig(path string) (*serviceWatcher, error) {
	// READ JSON
	watcher, err := readConfig(path)
	if err != nil {
		return nil, err
	}

	// VALIDATE CONFIG
	if err := validateConfig(watcher, MAX_CONCURRENT); err != nil {
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

func validateConfig(watcher *serviceWatcher, maxConcurrent int) error {

	// Step 1: Collect unique dependencies
	uniqueDeps := collectUniqueDependencies(watcher.Services)

	// Step 2: Validate dependencies concurrently
	if err := validateDependencies(uniqueDeps, maxConcurrent); err != nil {
		return err
	}

	// Step 3: Validate services concurrently
	if err := validateServices(watcher.Services, maxConcurrent); err != nil {
		return err
	}

	if err := validateEncryption(watcher); err != nil {
		return err
	}

	return nil
}

// validateDependencies concurrently validates all dependencies.
func validateDependencies(deps map[DependencyKey]Dependency, maxConcurrent int) error {
	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup
	errCh := make(chan error, len(deps))

	for key, dep := range deps {
		wg.Add(1)
		go func(d Dependency, k DependencyKey) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			if err := d.Validate(); err != nil {
				errCh <- fmt.Errorf("dependency %q validation failed: %w", k.Name, err)
			}
		}(dep, key)
	}

	wg.Wait()
	close(errCh)

	return collectErrors(errCh)
}

// validateServices concurrently validates all services.
func validateServices(services []Service, maxConcurrent int) error {
	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup
	errCh := make(chan error, len(services))

	for _, svc := range services {
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

	return collectErrors(errCh)
}

// collectErrors aggregates errors from the channel and returns a combined error.
func collectErrors(errCh <-chan error) error {
	var errs []error
	var b strings.Builder

	for err := range errCh {
		errs = append(errs, err)
	}

	if len(errs) == 0 {
		return nil
	}

	b.WriteString("validation errors:\n")
	for _, err := range errs {
		b.WriteString("- ")
		b.WriteString(err.Error())
		b.WriteString("\n")
	}

	return fmt.Errorf("%s\n(details: %w)", b.String(), errors.Join(errs...))
}

func validateEncryption(watcher *serviceWatcher) error {

	enc, err := crypto.NewEncryption(watcher.Email.EncryptionType)
	if err != nil {
		watcher.Email.Encryption, err = crypto.NewEncryption("")
		return fmt.Errorf("default encryption init failed: %w", err)
	}
	watcher.Email.Encryption = enc
	return nil
}
