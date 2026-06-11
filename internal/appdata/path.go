package appdata

import (
	"os"
	"path/filepath"
)

func Resolve(override string) (string, error) {
	if override != "" {
		return filepath.Abs(override)
	}

	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "fastproxy"), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "fastproxy"), nil
}

func Ensure(path string) error {
	for _, name := range []string{
		path,
		filepath.Join(path, "repository"),
		filepath.Join(path, "repository", "rule-source-indexes"),
	} {
		if err := os.MkdirAll(name, 0o755); err != nil {
			return err
		}
	}
	return nil
}
