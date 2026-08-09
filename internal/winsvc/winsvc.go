//go:build windows

// Package winsvc installs, controls, and runs local-ai as a Windows Service,
// sharing the same supervisor+gateway logic the foreground `serve` command
// uses rather than reimplementing it.
package winsvc

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"

	"local-ai/internal/config"
	"local-ai/internal/gateway"
	"local-ai/internal/keys"
	"local-ai/internal/supervisor"
)

const (
	ServiceName        = "LocalAI"
	ServiceDisplayName = "local-ai"
	serviceDescription = "Supervises llama-server and exposes it as a networked, multi-model LLM host."
)

// IsWindowsService reports whether the current process was launched by the
// Windows Service Control Manager.
func IsWindowsService() (bool, error) {
	return svc.IsWindowsService()
}

// Run is the service entrypoint: it blocks for the lifetime of the service.
func Run() error {
	return svc.Run(ServiceName, &handler{})
}

type handler struct{}

func (h *handler) Execute(args []string, r <-chan svc.ChangeRequest, s chan<- svc.Status) (bool, uint32) {
	s <- svc.Status{State: svc.StartPending}

	cfg, paths, err := config.Load(config.DataDir(""))
	if err != nil {
		return false, 1
	}
	store, err := keys.Load(paths.KeysPath)
	if err != nil {
		return false, 1
	}
	gw, err := gateway.New(cfg, store)
	if err != nil {
		return false, 1
	}
	sup := supervisor.New(cfg, paths)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 2)
	go func() { errCh <- sup.Run(ctx, nil) }()
	go func() { errCh <- gw.Run(ctx) }()

	s <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}

	for {
		select {
		case req := <-r:
			switch req.Cmd {
			case svc.Interrogate:
				s <- req.CurrentStatus
			case svc.Stop, svc.Shutdown:
				s <- svc.Status{State: svc.StopPending}
				sup.Stop()
				cancel()
				s <- svc.Status{State: svc.Stopped}
				return false, 0
			}
		case <-errCh:
			// supervisor or gateway exited on its own (shouldn't happen
			// outside shutdown, since supervisor restarts llama-server
			// itself); stop cleanly rather than leaving a half-running
			// service behind.
			s <- svc.Status{State: svc.StopPending}
			sup.Stop()
			cancel()
			s <- svc.Status{State: svc.Stopped}
			return false, 1
		}
	}
}

// Install registers local-ai as a Windows Service running exePath, with
// crash recovery (restart with backoff) and the requested startup type.
func Install(exePath string, startAutomatic bool) error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connecting to service manager (try an elevated shell): %w", err)
	}
	defer m.Disconnect()

	if existing, err := m.OpenService(ServiceName); err == nil {
		existing.Close()
		return fmt.Errorf("service %s is already installed", ServiceName)
	}

	startType := uint32(mgr.StartManual)
	if startAutomatic {
		startType = mgr.StartAutomatic
	}

	s, err := m.CreateService(ServiceName, exePath, mgr.Config{
		DisplayName: ServiceDisplayName,
		Description: serviceDescription,
		StartType:   startType,
	})
	if err != nil {
		return fmt.Errorf("creating service: %w", err)
	}
	defer s.Close()

	actions := []mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 5 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 10 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 30 * time.Second},
	}
	if err := s.SetRecoveryActions(actions, uint32((24 * time.Hour).Seconds())); err != nil {
		return fmt.Errorf("service created, but setting recovery actions failed: %w", err)
	}
	return nil
}

// Uninstall removes the local-ai Windows Service.
func Uninstall() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connecting to service manager (try an elevated shell): %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(ServiceName)
	if err != nil {
		return fmt.Errorf("service %s is not installed", ServiceName)
	}
	defer s.Close()
	return s.Delete()
}

// Start starts the installed service.
func Start() error {
	s, m, err := open()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	defer s.Close()
	return s.Start()
}

// Stop stops the installed service and waits (briefly) for it to actually
// reach the Stopped state.
func Stop() error {
	s, m, err := open()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	defer s.Close()

	if _, err := s.Control(svc.Stop); err != nil {
		return err
	}
	return waitForState(s, svc.Stopped, 15*time.Second)
}

// Restart stops then starts the installed service.
func Restart() error {
	if err := Stop(); err != nil {
		return err
	}
	return Start()
}

// SetStartType flips the SCM startup type without reinstalling.
func SetStartType(automatic bool) error {
	s, m, err := open()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	defer s.Close()

	cfg, err := s.Config()
	if err != nil {
		return err
	}
	if automatic {
		cfg.StartType = mgr.StartAutomatic
	} else {
		cfg.StartType = mgr.StartManual
	}
	return s.UpdateConfig(cfg)
}

// QueryStatus reports the service's current SCM state as a human string.
func QueryStatus() (string, error) {
	s, m, err := open()
	if err != nil {
		return "", err
	}
	defer m.Disconnect()
	defer s.Close()

	status, err := s.Query()
	if err != nil {
		return "", err
	}
	return stateString(status.State), nil
}

func open() (*mgr.Service, *mgr.Mgr, error) {
	m, err := mgr.Connect()
	if err != nil {
		return nil, nil, fmt.Errorf("connecting to service manager (try an elevated shell): %w", err)
	}
	s, err := m.OpenService(ServiceName)
	if err != nil {
		m.Disconnect()
		return nil, nil, fmt.Errorf("service %s is not installed", ServiceName)
	}
	return s, m, nil
}

func waitForState(s *mgr.Service, want svc.State, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		status, err := s.Query()
		if err != nil {
			return err
		}
		if status.State == want {
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for service to reach state %s", stateString(want))
}

func stateString(state svc.State) string {
	switch state {
	case svc.Stopped:
		return "stopped"
	case svc.StartPending:
		return "start pending"
	case svc.StopPending:
		return "stop pending"
	case svc.Running:
		return "running"
	case svc.ContinuePending:
		return "continue pending"
	case svc.PausePending:
		return "pause pending"
	case svc.Paused:
		return "paused"
	default:
		return "unknown"
	}
}
