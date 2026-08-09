// Package supervisor launches and monitors llama-server.exe in router mode,
// restarting it on unexpected exit.
package supervisor

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"local-ai/internal/config"
	"local-ai/internal/presets"
)

const (
	minBackoff = 1 * time.Second
	maxBackoff = 30 * time.Second
	// A process that survives this long is considered healthy again, so a
	// later crash restarts the backoff from minBackoff instead of continuing
	// to climb.
	healthyAfter = 60 * time.Second
)

// Supervisor owns the llama-server.exe child process lifecycle.
type Supervisor struct {
	cfg   *config.Config
	paths config.Paths

	mu      sync.Mutex
	cmd     *exec.Cmd
	stopped bool
}

// New creates a Supervisor for cfg/paths. Call Run to start supervising.
func New(cfg *config.Config, paths config.Paths) *Supervisor {
	return &Supervisor{cfg: cfg, paths: paths}
}

// Run generates presets.ini, then launches llama-server.exe and restarts it
// on unexpected exit until ctx is cancelled or Stop is called. extraOut, if
// non-nil, receives a copy of the child's combined output (used by the
// foreground `serve` command; the Windows Service passes nil and relies on
// the log file alone). Run blocks until the child has been asked to stop and
// has exited.
func (s *Supervisor) Run(ctx context.Context, extraOut io.Writer) error {
	if _, err := presets.Write(s.paths.PresetsPath, s.cfg.ModelOverrides); err != nil {
		return fmt.Errorf("writing presets: %w", err)
	}
	usePresets := len(s.cfg.ModelOverrides) > 0

	logPath := filepath.Join(s.paths.LogsDir, "llama-server.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("opening %s: %w", logPath, err)
	}
	defer logFile.Close()

	var out io.Writer = logFile
	if extraOut != nil {
		out = io.MultiWriter(logFile, extraOut)
	}

	backoff := minBackoff
	for {
		if ctx.Err() != nil {
			return nil
		}

		start := time.Now()
		args := s.buildArgs(usePresets)
		cmd := exec.CommandContext(ctx, s.cfg.LlamaServerPath, args...)
		cmd.Stdout = out
		cmd.Stderr = out

		s.mu.Lock()
		s.cmd = cmd
		s.mu.Unlock()

		fmt.Fprintf(out, "--- local-ai: starting %s %v ---\n", s.cfg.LlamaServerPath, redactArgs(args))
		runErr := cmd.Run()

		s.mu.Lock()
		stopped := s.stopped
		s.mu.Unlock()

		if ctx.Err() != nil || stopped {
			return nil
		}

		if time.Since(start) >= healthyAfter {
			backoff = minBackoff
		}
		fmt.Fprintf(out, "--- local-ai: llama-server exited (%v), restarting in %s ---\n", runErr, backoff)

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}

		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

// Stop requests the supervised process to exit and prevents further restarts.
func (s *Supervisor) Stop() {
	s.mu.Lock()
	s.stopped = true
	cmd := s.cmd
	s.mu.Unlock()

	if cmd == nil || cmd.Process == nil {
		return
	}
	if err := cmd.Process.Kill(); err != nil {
		log.Printf("local-ai: killing llama-server: %v", err)
	}
}

func (s *Supervisor) buildArgs(usePresets bool) []string {
	args := []string{
		"--host", s.cfg.InternalHost,
		"--port", strconv.Itoa(s.cfg.InternalPort),
		"--models-dir", s.cfg.ModelsDir,
		"--models-max", strconv.Itoa(s.cfg.ModelsMax),
		"--models-autoload",
		"--sleep-idle-seconds", strconv.Itoa(s.cfg.IdleTimeoutSeconds),
		"--api-key", s.cfg.InternalAPIKey,
	}
	if usePresets {
		args = append(args, "--models-preset", s.paths.PresetsPath)
	}
	return args
}

// redactArgs hides the internal API key from logs.
func redactArgs(args []string) []string {
	out := make([]string, len(args))
	copy(out, args)
	for i, a := range out {
		if a == "--api-key" && i+1 < len(out) {
			out[i+1] = "***"
		}
	}
	return out
}
