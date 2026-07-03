package core

import (
	"context"
	"testing"

	"github.com/atlantis-mk/FastProxy-Server/internal/repository"
)

type supervisorTestAdapter struct {
	command []string
}

func (a supervisorTestAdapter) Core() repository.Core { return repository.CoreMihomo }
func (a supervisorTestAdapter) GeneratedConfigPath(dataDir string) string {
	return dataDir + "/test.yaml"
}
func (a supervisorTestAdapter) RuntimeBaseURL(runtime RuntimeConfig) string { return "" }
func (a supervisorTestAdapter) StartCommand(binaryPath string, configPath string) []string {
	return a.command
}
func (a supervisorTestAdapter) Validate(ctx context.Context, binaryPath string, configPath string) error {
	return nil
}
func (a supervisorTestAdapter) HealthCheck(ctx context.Context, runtime RuntimeConfig) error {
	return nil
}

func TestSupervisorStopWaitsForProcessExit(t *testing.T) {
	supervisor := NewSupervisor(nil)
	adapter := supervisorTestAdapter{command: []string{"/bin/sh", "-c", "sleep 30"}}

	if err := supervisor.Start(context.Background(), adapter, "", "", RuntimeConfig{}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	supervisor.mu.Lock()
	cmd := supervisor.cmd
	supervisor.mu.Unlock()
	if cmd == nil {
		t.Fatal("supervisor command is nil after Start()")
	}

	if err := supervisor.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if cmd.ProcessState == nil {
		t.Fatal("Stop() returned before the old process was waited")
	}
	if status := supervisor.Status(); status.State != "stopped" {
		t.Fatalf("Status().State = %q, want stopped", status.State)
	}
}

func TestSupervisorRestartWaitsForPreviousProcessExit(t *testing.T) {
	supervisor := NewSupervisor(nil)
	adapter := supervisorTestAdapter{command: []string{"/bin/sh", "-c", "sleep 30"}}

	if err := supervisor.Start(context.Background(), adapter, "", "", RuntimeConfig{}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	supervisor.mu.Lock()
	previousCmd := supervisor.cmd
	supervisor.mu.Unlock()
	if previousCmd == nil {
		t.Fatal("supervisor command is nil after Start()")
	}

	if err := supervisor.Restart(context.Background(), adapter, "", "", RuntimeConfig{}); err != nil {
		t.Fatalf("Restart() error = %v", err)
	}
	defer supervisor.Stop(context.Background())

	if previousCmd.ProcessState == nil {
		t.Fatal("Restart() started the new process before waiting for the previous process")
	}

	supervisor.mu.Lock()
	currentCmd := supervisor.cmd
	supervisor.mu.Unlock()
	if currentCmd == nil {
		t.Fatal("supervisor command is nil after Restart()")
	}
	if currentCmd == previousCmd {
		t.Fatal("Restart() reused the previous command")
	}
	if status := supervisor.Status(); status.State != "running" {
		t.Fatalf("Status().State = %q, want running", status.State)
	}
}
