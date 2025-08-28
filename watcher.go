package main

import (
	"log"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/debug"
)

type serviceWatcher struct {
	Email    EmailConfig `json:"email"`
	Services []Service   `json:"services"`
}

type RecoveryContext struct {
	ServiceName string
	Services    ServiceManager
	Email       EmailConfig
}

func runWatcherService(name string, isDebug bool, watcher *serviceWatcher) {
	if isDebug {
		err := debug.Run(name, watcher)
		if err != nil {
			log.Fatalln("running service in debug mode. error: %w", err)
		}
	} else {
		err := svc.Run(name, watcher)
		if err != nil {
			log.Fatalln("running service in service control mode. error: %w", err)
		}
	}
}

func (m *serviceWatcher) Execute(args []string, r <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	const cmdsAccepted = svc.AcceptStop | svc.AcceptShutdown | svc.AcceptPauseAndContinue

	tick := time.Tick(30 * time.Second)
	status <- svc.Status{State: svc.StartPending}
	status <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}

	for {
		select {
		case <-tick:
			log.Print("Running Service Health Checks...!")
			m.runHealthChecks()
		case c := <-r:
			switch c.Cmd {
			case svc.Interrogate:
				status <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				log.Print("Shutting service...!")
				status <- svc.Status{State: svc.StopPending}
				return false, 1
			case svc.Pause:
				status <- svc.Status{State: svc.Paused, Accepts: cmdsAccepted}
			case svc.Continue:
				status <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}
			default:
				log.Printf("Unexepcted service control request %+v", c)
			}
		}
	}
}

func (sw *serviceWatcher) runHealthChecks() {
	cache := newHealthCache()
	now := time.Now()

	for _, svc := range sw.Services {
		sw.checkServiceHealth(&svc, cache, now)
	}
}

func (sw *serviceWatcher) checkServiceHealth(svc *Service, cache *healthCache, now time.Time) {
	results, err := svc.HealthCheck(MAX_CONCURRENT, cache)
	if err != nil {
		log.Printf("Health check failed for %s: %v", svc.Name, err)
		return
	}

	for _, result := range results {
		sw.handleHealthCheckResult(svc, result, now)
	}
}

func (sw *serviceWatcher) handleHealthCheckResult(svc *Service, result healthStatus, now time.Time) {
	if result.Running {
		if result.Type == "service" {
			svc.ResetHealthyTracking()
			return
		}
	}

	// Skip Dependency Healing
	if result.Type == "dependency" {
		// Dependency Handling
		return
	}

	log.Printf("Unhealthy: %s (%v)", result.Name, result.Err)

	// First time unhealthy detected, initialise
	if svc.CurrentQueryCount() == 0 {
		svc.IncrementQueryCount(now)
		return
	}

	// Are we in a Debounce Widnow
	if svc.InsideDebounceWindow(now) {
		return
	}
	// Outside Debounce
	svc.IncrementQueryCount(now)

	// Check Exceeded Max Attempts
	if svc.ExceededQueryCount() {
		sw.attemptRecovery(svc)
	}
}

func (sw *serviceWatcher) attemptRecovery(svc *Service) {
	// Recovery Functionality
	ctx := RecoveryContext{
		ServiceName: svc.Name,
		Services:    mustLocalServiceManager(),
		Email:       sw.Email,
	}

	if err := svc.Recover(ctx); err != nil {
		log.Printf("Recovery failed for %s: %v", svc.Name, err)
	}

	// Reset Retry after Recovery Attempt
	svc.ResetHealthyTracking()
}

func mustLocalServiceManager() ServiceManager {
	mgr, err := NewLocalServiceManager()
	if err != nil {
		log.Fatalf("failed to connect to local SCM: %v", err)
	}
	return mgr
}

func (sw *serviceWatcher) InjectResolver(resolver ServiceManagerResolver) {
	for i := range sw.Services {
		sw.Services[i].Resolver = resolver

		for j := range sw.Services[i].Dependencies {
			sw.Services[i].Dependencies[j].Resolver = resolver
		}
	}
}
