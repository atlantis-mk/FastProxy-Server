package core

import (
	"compress/gzip"
	"embed"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/atlantis-mk/FastProxy-Server/internal/repository"
)

//go:embed embedded_binaries/*.gz
var embeddedBinaryFS embed.FS

var execLookPath = exec.LookPath

func installEmbeddedBinary(dataDir string, core repository.Core) (string, error) {
	name := CoreBinaryName(core)
	source := embeddedBinaryPath(core)
	if source == "" {
		return "", fmt.Errorf("no embedded binary for %s/%s/%s", core, runtime.GOOS, runtime.GOARCH)
	}

	dir := filepath.Join(dataDir, "cores", string(core), "embedded")
	target := filepath.Join(dir, name)
	if info, err := os.Stat(target); err == nil && !info.IsDir() && executableBinary(target) {
		return target, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if err := extractEmbeddedBinary(source, target); err != nil {
		return "", err
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(target, 0o755); err != nil {
			return "", err
		}
	}
	return target, nil
}

func embeddedBinaryPath(core repository.Core) string {
	switch core {
	case repository.CoreMihomo:
		return "embedded_binaries/mihomo-" + runtime.GOOS + "-" + runtime.GOARCH + ".gz"
	case repository.CoreSingBox:
		return "embedded_binaries/sing-box-" + runtime.GOOS + "-" + runtime.GOARCH + ".gz"
	default:
		return ""
	}
}

func extractEmbeddedBinary(source string, target string) error {
	data, err := embeddedBinaryFS.Open(source)
	if err != nil {
		return err
	}
	defer data.Close()

	reader, err := gzip.NewReader(data)
	if err != nil {
		return err
	}
	defer reader.Close()

	tmp := target + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, reader)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
