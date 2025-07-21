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
		results, err := svc.HealthCheck(MAX_CONCURRENT, cache)
		if err != nil {
			log.Printf("Health check failed for %s: %v", svc.Name, err)
			continue
		}

		for _, result := range results {

			if result.Running {
				if result.Type == "service" {
					svc.ResetHealthyTracking()
					continue
				}
			}

			// Skip Dependency Healing
			if result.Type == "dependency" {
				// Dependency Handling
				continue
			}

			log.Printf("Unhealthy: %s (%v)", result.Name, result.Err)

			// First time unhealthy detected, initialise
			if svc.CurrentAttempt() == 0 {
				svc.IncrementRetry(now)
				continue
			}

			// Are we in a Debounce Widnow
			if svc.InsideDebounceWindow(now) {
				continue
			}
			// Outside Debounce
			svc.IncrementRetry(now)

			// Check Exceeded Max Attempts
			if svc.ExceededAttempts() {

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
		}
	}
}

func mustLocalServiceManager() ServiceManager {
	mgr, err := NewLocalServiceManager()
	if err != nil {
		log.Fatalf("failed to connect to local SCM: %v", err)
	}
	return mgr
}
