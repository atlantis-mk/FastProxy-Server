package appconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type SettingsStore struct {
	mu   sync.Mutex
	path string
}

type Settings struct {
	GitHubToken string `json:"githubToken,omitempty"`
}

func NewSettingsStore(dataDir string) *SettingsStore {
	return &SettingsStore{path: filepath.Join(dataDir, "settings.json")}
}

func (s *SettingsStore) Get() (Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.read()
}

func (s *SettingsStore) Save(settings Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	settings.GitHubToken = strings.TrimSpace(settings.GitHubToken)
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o600)
}

func (s *SettingsStore) GitHubToken() string {
	settings, err := s.Get()
	if err != nil {
		return ""
	}
	return settings.GitHubToken
}

func (s *SettingsStore) read() (Settings, error) {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return Settings{}, nil
	}
	if err != nil {
		return Settings{}, err
	}
	var settings Settings
	if err := json.Unmarshal(data, &settings); err != nil {
		return Settings{}, err
	}
	settings.GitHubToken = strings.TrimSpace(settings.GitHubToken)
	return settings, nil
}
