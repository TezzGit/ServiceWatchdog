package main

import (
	"log"
	"sync"

	"golang.org/x/sys/windows/svc"
)

type HealthStatus struct {
	Name      string
	IsService bool
	Healthy   bool
	Err       error
}

// Cache Dependency Health Status to Avoid Per Service Call
type HealthCache struct {
	mu      sync.Mutex
	results map[DependencyKey]HealthStatus
}

func newHealthCache() *HealthCache {
	return &HealthCache{results: make(map[DependencyKey]HealthStatus)}
}

func (hc *HealthCache) GetOrRun(dep Dependency, fn func() (bool, error)) HealthStatus {
	key := DependencyKey(dep)

	hc.mu.Lock()
	if result, exists := hc.results[key]; exists {
		hc.mu.Unlock()
		return result
	}
	hc.mu.Unlock()

	// Run and store
	healthy, err := fn()
	status := HealthStatus{
		Name:      dep.Name,
		IsService: false,
		Healthy:   healthy,
		Err:       err,
	}

	hc.mu.Lock()
	hc.results[key] = status
	hc.mu.Unlock()
	return status
}

func isHealthyState(name string, state svc.State) bool {
	switch state {
	case svc.Running:
		return true
	default:
		log.Printf("Service %q is in an unhealthy state: %v", name, state)
		return false
	}
}
