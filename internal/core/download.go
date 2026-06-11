package core

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/atlantis-mk/FastProxy-Server/internal/repository"
)

const githubAPI = "https://api.github.com/repos"

type releaseAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

type githubRelease struct {
	TagName string         `json:"tag_name"`
	Assets  []releaseAsset `json:"assets"`
}

type BinaryCache struct {
	Path    string `json:"path,omitempty"`
	Version string `json:"version,omitempty"`
	Exists  bool   `json:"exists"`
}

type UpdateInfo struct {
	Core            repository.Core `json:"core"`
	CurrentVersion  string          `json:"currentVersion,omitempty"`
	LatestVersion   string          `json:"latestVersion"`
	UpdateAvailable bool            `json:"updateAvailable"`
	Cached          bool            `json:"cached"`
	AssetName       string          `json:"assetName"`
}

type TokenProvider func() string

var githubTokenProvider TokenProvider

func SetGitHubTokenProvider(provider TokenProvider) {
	githubTokenProvider = provider
}

func ValidateCore(core repository.Core) error {
	switch core {
	case repository.CoreMihomo, repository.CoreSingBox:
		return nil
	default:
		return fmt.Errorf("unsupported core %q", core)
	}
}

func ResolveBinary(ctx context.Context, dataDir string, core repository.Core, configuredPath string) (string, error) {
	if err := ValidateCore(core); err != nil {
		return "", err
	}
	if strings.TrimSpace(configuredPath) != "" {
		return strings.TrimSpace(configuredPath), nil
	}
	if cache := CachedBinary(dataDir, core); cache.Exists {
		return cache.Path, nil
	}
	release, err := latestRelease(ctx, core)
	if err != nil {
		return "", err
	}
	return downloadRelease(ctx, dataDir, core, release)
}

func CheckUpdate(ctx context.Context, dataDir string, core repository.Core) (UpdateInfo, error) {
	if err := ValidateCore(core); err != nil {
		return UpdateInfo{}, err
	}
	cache := CachedBinary(dataDir, core)
	release, err := latestRelease(ctx, core)
	if err != nil {
		return UpdateInfo{}, err
	}
	asset, err := selectAsset(core, release.Assets)
	if err != nil {
		return UpdateInfo{}, err
	}
	return UpdateInfo{
		Core:            core,
		CurrentVersion:  cache.Version,
		LatestVersion:   release.TagName,
		UpdateAvailable: cache.Version != release.TagName,
		Cached:          cache.Exists,
		AssetName:       asset.Name,
	}, nil
}

func DownloadLatest(ctx context.Context, dataDir string, core repository.Core) (UpdateInfo, error) {
	if err := ValidateCore(core); err != nil {
		return UpdateInfo{}, err
	}
	release, err := latestRelease(ctx, core)
	if err != nil {
		return UpdateInfo{}, err
	}
	asset, err := selectAsset(core, release.Assets)
	if err != nil {
		return UpdateInfo{}, err
	}
	if _, err := downloadRelease(ctx, dataDir, core, release); err != nil {
		return UpdateInfo{}, err
	}
	cache := CachedBinary(dataDir, core)
	return UpdateInfo{
		Core:            core,
		CurrentVersion:  cache.Version,
		LatestVersion:   release.TagName,
		UpdateAvailable: false,
		Cached:          cache.Exists,
		AssetName:       asset.Name,
	}, nil
}

func InstallLocal(dataDir string, core repository.Core, filename string, reader io.Reader) (BinaryCache, error) {
	if err := ValidateCore(core); err != nil {
		return BinaryCache{}, err
	}
	version := "local-" + time.Now().UTC().Format("20060102-150405")
	dir := filepath.Join(dataDir, "cores", string(core), version)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return BinaryCache{}, err
	}
	name := filepath.Base(filename)
	if name == "." || name == string(filepath.Separator) {
		name = CoreBinaryName(core)
	}
	uploadPath := filepath.Join(dir, name)
	if err := writeExtracted(uploadPath, reader); err != nil {
		return BinaryCache{}, err
	}
	binaryPath := filepath.Join(dir, CoreBinaryName(core))
	if archiveName(strings.ToLower(name)) {
		if err := extractBinary(uploadPath, CoreBinaryName(core), binaryPath); err != nil {
			return BinaryCache{}, err
		}
	} else if name != CoreBinaryName(core) {
		if err := os.Rename(uploadPath, binaryPath); err != nil {
			return BinaryCache{}, err
		}
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(binaryPath, 0o755); err != nil {
			return BinaryCache{}, err
		}
	}
	return BinaryCache{Path: binaryPath, Version: version, Exists: true}, nil
}

func CachedBinary(dataDir string, core repository.Core) BinaryCache {
	binaryName := CoreBinaryName(core)
	coreDir := filepath.Join(dataDir, "cores", string(core))
	entries, err := os.ReadDir(coreDir)
	if err != nil {
		return BinaryCache{}
	}
	for i := len(entries) - 1; i >= 0; i-- {
		if !entries[i].IsDir() {
			continue
		}
		path := filepath.Join(coreDir, entries[i].Name(), binaryName)
		if info, err := os.Stat(path); err == nil && !info.IsDir() && executableBinary(path) {
			return BinaryCache{Path: path, Version: entries[i].Name(), Exists: true}
		}
	}
	return BinaryCache{}
}

func CoreBinaryName(core repository.Core) string {
	binaryName := string(core)
	if core == repository.CoreSingBox {
		binaryName = "sing-box"
	}
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}
	return binaryName
}

func downloadRelease(ctx context.Context, dataDir string, core repository.Core, release githubRelease) (string, error) {
	binaryName := CoreBinaryName(core)
	asset, err := selectAsset(core, release.Assets)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(dataDir, "cores", string(core), release.TagName)
	binaryPath := filepath.Join(dir, binaryName)
	if info, err := os.Stat(binaryPath); err == nil && !info.IsDir() && executableBinary(binaryPath) {
		return binaryPath, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	archivePath := filepath.Join(dir, asset.Name)
	if err := downloadFile(ctx, asset.URL, archivePath); err != nil {
		return "", err
	}
	if err := extractBinary(archivePath, binaryName, binaryPath); err != nil {
		return "", err
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(binaryPath, 0o755); err != nil {
			return "", err
		}
	}
	return binaryPath, nil
}

func executableBinary(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()

	header := make([]byte, 4)
	n, err := io.ReadFull(file, header)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return false
	}
	if n < 2 {
		return false
	}
	if header[0] == 0x1f && header[1] == 0x8b {
		return false
	}
	if n >= 4 && string(header[:4]) == "PK\x03\x04" {
		return false
	}
	if header[0] == '#' && header[1] == '!' {
		return true
	}
	if n >= 4 && string(header[:4]) == "\x7fELF" {
		return true
	}
	if header[0] == 'M' && header[1] == 'Z' {
		return true
	}
	if n >= 4 {
		magic := []byte{header[0], header[1], header[2], header[3]}
		switch string(magic) {
		case "\xfe\xed\xfa\xce", "\xce\xfa\xed\xfe", "\xfe\xed\xfa\xcf", "\xcf\xfa\xed\xfe", "\xca\xfe\xba\xbe", "\xbe\xba\xfe\xca":
			return true
		}
	}
	return false
}

func latestRelease(ctx context.Context, core repository.Core) (githubRelease, error) {
	repo := "MetaCubeX/mihomo"
	if core == repository.CoreSingBox {
		repo = "SagerNet/sing-box"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, githubAPI+"/"+repo+"/releases/latest", nil)
	if err != nil {
		return githubRelease{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if token := githubToken(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return githubRelease{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		if resp.StatusCode == http.StatusForbidden {
			return githubRelease{}, fmt.Errorf("latest release request failed: %s; GitHub may be rate-limiting unauthenticated requests, set GITHUB_TOKEN or configure a local core binary path", resp.Status)
		}
		return githubRelease{}, fmt.Errorf("latest release request failed: %s", resp.Status)
	}
	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return githubRelease{}, err
	}
	if release.TagName == "" {
		return githubRelease{}, errors.New("latest release did not include a tag")
	}
	return release, nil
}

func githubToken() string {
	if githubTokenProvider != nil {
		if token := strings.TrimSpace(githubTokenProvider()); token != "" {
			return token
		}
	}
	return strings.TrimSpace(os.Getenv("GITHUB_TOKEN"))
}

func selectAsset(core repository.Core, assets []releaseAsset) (releaseAsset, error) {
	osName, archName, err := assetPlatform(core)
	if err != nil {
		return releaseAsset{}, err
	}
	for _, asset := range assets {
		name := strings.ToLower(asset.Name)
		if !archiveName(name) || strings.Contains(name, "checksum") || strings.Contains(name, ".sha") {
			continue
		}
		if strings.Contains(name, osName) && strings.Contains(name, archName) {
			return asset, nil
		}
	}
	return releaseAsset{}, fmt.Errorf("no %s release asset found for %s/%s", core, runtime.GOOS, runtime.GOARCH)
}

func assetPlatform(core repository.Core) (string, string, error) {
	osName := runtime.GOOS
	archName := runtime.GOARCH
	switch runtime.GOOS {
	case "darwin", "linux", "windows":
	default:
		return "", "", fmt.Errorf("unsupported OS %q", runtime.GOOS)
	}
	switch runtime.GOARCH {
	case "amd64":
		archName = "amd64"
	case "arm64":
		archName = "arm64"
	default:
		return "", "", fmt.Errorf("unsupported architecture %q", runtime.GOARCH)
	}
	return osName, archName, nil
}

func archiveName(name string) bool {
	return strings.HasSuffix(name, ".zip") || strings.HasSuffix(name, ".tar.gz") || strings.HasSuffix(name, ".tgz") || strings.HasSuffix(name, ".gz")
}

func downloadFile(ctx context.Context, url string, destination string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := (&http.Client{Timeout: 5 * time.Minute}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("download failed: %s", resp.Status)
	}
	tmp := destination + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, resp.Body)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	return os.Rename(tmp, destination)
}

func extractBinary(archivePath string, binaryName string, destination string) error {
	lower := strings.ToLower(archivePath)
	switch {
	case strings.HasSuffix(lower, ".zip"):
		return extractZipBinary(archivePath, binaryName, destination)
	case strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz"):
		return extractTarGzBinary(archivePath, binaryName, destination)
	case strings.HasSuffix(lower, ".gz"):
		return extractGzipBinary(archivePath, destination)
	default:
		return fmt.Errorf("unsupported archive format %q", archivePath)
	}
}

func extractZipBinary(archivePath string, binaryName string, destination string) error {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer reader.Close()
	for _, file := range reader.File {
		if file.FileInfo().IsDir() || filepath.Base(file.Name) != binaryName {
			continue
		}
		in, err := file.Open()
		if err != nil {
			return err
		}
		defer in.Close()
		return writeExtracted(destination, in)
	}
	return fmt.Errorf("binary %q not found in %s", binaryName, archivePath)
}

func extractTarGzBinary(archivePath string, binaryName string, destination string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if header.Typeflag == tar.TypeReg && filepath.Base(header.Name) == binaryName {
			return writeExtracted(destination, reader)
		}
	}
	return fmt.Errorf("binary %q not found in %s", binaryName, archivePath)
}

func extractGzipBinary(archivePath string, destination string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gz.Close()
	return writeExtracted(destination, gz)
}

func writeExtracted(destination string, reader io.Reader) error {
	tmp := destination + ".tmp"
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
	return os.Rename(tmp, destination)
}
