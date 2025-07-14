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

type SMTPEmailSender struct {
	Config EmailConfig
}

func (s *SMTPEmailSender) Send(subject, body string) error {
	// Send an Email
	return nil
}

func runWacherService(name string, isDebug bool, watcher *serviceWatcher) {
	if isDebug {
		err := debug.Run(name, watcher)
		if err != nil {
			log.Fatalln("Error running service in debug mode.")
		}
	} else {
		err := svc.Run(name, watcher)
		if err != nil {
			log.Fatalln("Error running service in Service Control mode.")
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
	for _, svc := range sw.Services {
		results, err := svc.HealthCheck(MAX_CONCURRENT, cache)
		if err != nil {
			log.Printf("Health check failed for %s: %v", svc.Name, err)
			continue
		}

		for _, result := range results {
			if !result.Running {
				log.Printf("Unhealthy: %s (%v)", result.Name, result.Err)

				// Recovery Functionality

			}
		}
	}
}
