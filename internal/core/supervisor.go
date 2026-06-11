package core

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/atlantis-mk/FastProxy-Server/internal/repository"
)

type Supervisor struct {
	mu     sync.Mutex
	logger *slog.Logger
	cmd    *exec.Cmd
	status RuntimeStatus
	logs   []LogEntry
}

type RuntimeStatus struct {
	Core      repository.Core `json:"core,omitempty"`
	State     string          `json:"state"`
	StartedAt *time.Time      `json:"startedAt,omitempty"`
	Error     string          `json:"error,omitempty"`
}

type LogEntry struct {
	Time    time.Time `json:"time"`
	Stream  string    `json:"stream"`
	Message string    `json:"message"`
}

func NewSupervisor(logger *slog.Logger) *Supervisor {
	return &Supervisor{logger: logger, status: RuntimeStatus{State: "stopped"}}
}

func (s *Supervisor) Status() RuntimeStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

func (s *Supervisor) Start(ctx context.Context, adapter Adapter, binaryPath string, configPath string, runtime RuntimeConfig) error {
	s.mu.Lock()
	if s.cmd != nil && s.cmd.Process != nil {
		s.mu.Unlock()
		return errors.New("core is already running")
	}
	args := adapter.StartCommand(binaryPath, configPath)
	if len(args) == 0 || args[0] == "" {
		s.mu.Unlock()
		return errors.New("core binary path is not configured")
	}
	cmd := exec.Command(args[0], args[1:]...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		s.mu.Unlock()
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		s.mu.Unlock()
		return err
	}
	if err := cmd.Start(); err != nil {
		s.mu.Unlock()
		return err
	}
	now := time.Now().UTC()
	s.cmd = cmd
	s.setStatusLocked(RuntimeStatus{Core: adapter.Core(), State: "starting", StartedAt: &now})
	s.mu.Unlock()

	go s.capture("stdout", stdout)
	go s.capture("stderr", stderr)
	go s.wait(cmd)

	deadline, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := adapter.HealthCheck(deadline, runtime); err != nil {
		failure := fmt.Errorf("health check failed: %w", err)
		s.stopAfterStartFailure(adapter.Core(), failure)
		return failure
	}
	s.setRunning(adapter.Core())
	return nil
}

func (s *Supervisor) Stop(ctx context.Context) error {
	s.mu.Lock()
	cmd := s.cmd
	if cmd == nil || cmd.Process == nil {
		s.setStatusLocked(RuntimeStatus{State: "stopped"})
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	_ = ctx
	if err := cmd.Process.Kill(); err != nil {
		return err
	}

	s.mu.Lock()
	s.cmd = nil
	s.setStatusLocked(RuntimeStatus{State: "stopped"})
	s.mu.Unlock()
	return nil
}

func (s *Supervisor) Restart(ctx context.Context, adapter Adapter, binaryPath string, configPath string, runtime RuntimeConfig) error {
	if err := s.Stop(ctx); err != nil {
		return err
	}
	return s.Start(ctx, adapter, binaryPath, configPath, runtime)
}

func (s *Supervisor) capture(stream string, reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		entry := LogEntry{Time: time.Now().UTC(), Stream: stream, Message: scanner.Text()}
		s.appendLog(entry)
		s.writeConsoleLog(entry)
	}
}

func (s *Supervisor) writeConsoleLog(entry LogEntry) {
	if entry.Stream == "stderr" {
		_, _ = fmt.Fprintln(os.Stderr, entry.Message)
		return
	}
	_, _ = fmt.Fprintln(os.Stdout, entry.Message)
}

func (s *Supervisor) wait(cmd *exec.Cmd) {
	err := cmd.Wait()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd == cmd {
		s.cmd = nil
		if err != nil {
			status := s.status
			status.State = "failed"
			status.Error = err.Error()
			s.setStatusLocked(status)
		} else {
			s.setStatusLocked(RuntimeStatus{State: "stopped"})
		}
	}
}

func (s *Supervisor) appendLog(entry LogEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logs = append(s.logs, entry)
	if len(s.logs) > 500 {
		s.logs = s.logs[len(s.logs)-500:]
	}
}

func (s *Supervisor) stopAfterStartFailure(core repository.Core, failure error) {
	s.mu.Lock()
	cmd := s.cmd
	s.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	s.mu.Lock()
	if s.cmd == cmd {
		s.cmd = nil
	}
	s.setStatusLocked(RuntimeStatus{Core: core, State: "failed", Error: failure.Error()})
	s.mu.Unlock()
}

func (s *Supervisor) setRunning(core repository.Core) {
	s.mu.Lock()
	defer s.mu.Unlock()
	status := s.status
	status.Core = core
	status.State = "running"
	status.Error = ""
	s.setStatusLocked(status)
}

func (s *Supervisor) setStatusLocked(status RuntimeStatus) {
	s.status = status
}
