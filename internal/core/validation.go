package core

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"time"
)

func runValidation(ctx context.Context, binaryPath string, args []string, configPath string) error {
	deadline, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(deadline, binaryPath, args...)
	cmd.Dir = filepath.Dir(configPath)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %s", err, output.String())
	}
	return nil
}
