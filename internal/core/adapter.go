package core

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/atlantis-mk/FastProxy-Server/internal/repository"
)

type RuntimeConfig struct {
	ExternalController string
	Secret             string
}

type Adapter interface {
	Core() repository.Core
	GeneratedConfigPath(dataDir string) string
	RuntimeBaseURL(runtime RuntimeConfig) string
	StartCommand(binaryPath string, configPath string) []string
	Validate(ctx context.Context, binaryPath string, configPath string) error
	HealthCheck(ctx context.Context, runtime RuntimeConfig) error
}

func For(core repository.Core) (Adapter, error) {
	switch core {
	case repository.CoreMihomo:
		return Mihomo{}, nil
	case repository.CoreSingBox:
		return SingBox{}, nil
	default:
		return nil, fmt.Errorf("unsupported core %q", core)
	}
}

type Mihomo struct{}

func (Mihomo) Core() repository.Core { return repository.CoreMihomo }
func (Mihomo) GeneratedConfigPath(dataDir string) string {
	return filepath.Join(dataDir, "runtime", "mihomo-active.yaml")
}
func (Mihomo) RuntimeBaseURL(runtime RuntimeConfig) string {
	return (&url.URL{Scheme: "http", Host: runtime.ExternalController}).String()
}
func (Mihomo) StartCommand(binaryPath string, configPath string) []string {
	return []string{binaryPath, "-d", filepath.Dir(configPath), "-f", configPath}
}
func (Mihomo) Validate(ctx context.Context, binaryPath string, configPath string) error {
	return runValidation(ctx, binaryPath, []string{"-d", filepath.Dir(configPath), "-t", "-f", configPath}, configPath)
}
func (m Mihomo) HealthCheck(ctx context.Context, runtime RuntimeConfig) error {
	return httpHealth(ctx, m.RuntimeBaseURL(runtime)+"/version", runtime.Secret)
}

type SingBox struct{}

func (SingBox) Core() repository.Core { return repository.CoreSingBox }
func (SingBox) GeneratedConfigPath(dataDir string) string {
	return filepath.Join(dataDir, "runtime", "sing-box-active.json")
}
func (SingBox) RuntimeBaseURL(runtime RuntimeConfig) string {
	return (&url.URL{Scheme: "http", Host: runtime.ExternalController}).String()
}
func (SingBox) StartCommand(binaryPath string, configPath string) []string {
	return []string{binaryPath, "run", "-c", configPath}
}
func (SingBox) Validate(ctx context.Context, binaryPath string, configPath string) error {
	return runValidation(ctx, binaryPath, []string{"check", "-c", configPath}, configPath)
}
func (s SingBox) HealthCheck(ctx context.Context, runtime RuntimeConfig) error {
	return httpHealth(ctx, s.RuntimeBaseURL(runtime)+"/version", runtime.Secret)
}

func httpHealth(ctx context.Context, target string, secret string) error {
	client := http.Client{Timeout: 2 * time.Second}
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	secret = strings.TrimSpace(secret)
	var lastErr error
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if err != nil {
			return err
		}
		if secret != "" {
			req.Header.Set("Authorization", "Bearer "+secret)
		}
		resp, err := client.Do(req)
		if err == nil {
			if resp.StatusCode < http.StatusBadRequest {
				resp.Body.Close()
				return nil
			}
			err = fmt.Errorf("health endpoint returned %s", resp.Status)
			resp.Body.Close()
		}
		lastErr = err

		select {
		case <-ctx.Done():
			if lastErr != nil {
				return lastErr
			}
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
