package main

import (
	"log"
	"sync"

	"golang.org/x/sys/windows/svc"
)

type healthStatus struct {
	Name      string
	IsService bool
	Running   bool
	Err       error
}

// Cache Dependency Health Status to Avoid Per Service Call
type healthCache struct {
	mu      sync.Mutex
	results map[DependencyKey]healthStatus
}

func newHealthCache() *healthCache {
	return &healthCache{results: make(map[DependencyKey]healthStatus)}
}

func (hc *healthCache) GetOrRun(dep Dependency, fn func() (bool, error)) healthStatus {
	key := DependencyKey(dep)

	hc.mu.Lock()
	if result, exists := hc.results[key]; exists {
		hc.mu.Unlock()
		return result
	}
	hc.mu.Unlock()

	// Run and store
	healthy, err := fn()
	status := healthStatus{
		Name:      dep.Name,
		IsService: false,
		Running:   healthy,
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
