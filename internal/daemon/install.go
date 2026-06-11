package daemon

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

const (
	defaultAddr       = "127.0.0.1:43171"
	defaultDataDir    = "/Library/Application Support/FastProxy"
	defaultHelperPath = "/Library/PrivilegedHelperTools/fastproxy-server"
	defaultLabel      = "com.fastproxy.server"
	defaultLogDir     = "/Library/Logs/FastProxy"
	defaultLogLevel   = "info"
	defaultPlistPath  = "/Library/LaunchDaemons/com.fastproxy.server.plist"
)

type installConfig struct {
	Addr              string
	DataDir           string
	HelperPath        string
	Label             string
	LogDir            string
	LogLevel          string
	MihomoBinaryPath  string
	NoStart           bool
	PlistPath         string
	SingBoxBinaryPath string
}

func defaultInstallConfig() installConfig {
	return installConfig{
		Addr:       env("FASTPROXY_SERVER_ADDR", defaultAddr),
		DataDir:    env("FASTPROXY_SERVER_DATA_DIR", defaultDataDir),
		HelperPath: defaultHelperPath,
		Label:      defaultLabel,
		LogDir:     defaultLogDir,
		LogLevel:   env("FASTPROXY_SERVER_LOG_LEVEL", defaultLogLevel),
		PlistPath:  defaultPlistPath,
	}
}

func Install(args []string, stdout io.Writer, stderr io.Writer) error {
	if runtime.GOOS != "darwin" {
		return errors.New("install is only supported on macOS")
	}
	if os.Geteuid() != 0 {
		return errors.New("install requires root privileges; run sudo fastproxy install")
	}

	cfg, err := parseInstallConfig(args, stderr)
	if err != nil {
		return err
	}

	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve current executable: %w", err)
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return fmt.Errorf("resolve executable symlink: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(cfg.HelperPath), 0o755); err != nil {
		return fmt.Errorf("create helper directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(cfg.PlistPath), 0o755); err != nil {
		return fmt.Errorf("create launch daemon directory: %w", err)
	}
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return fmt.Errorf("create data directory: %w", err)
	}
	if err := os.MkdirAll(cfg.LogDir, 0o755); err != nil {
		return fmt.Errorf("create log directory: %w", err)
	}

	if err := copyExecutable(executable, cfg.HelperPath); err != nil {
		return err
	}
	plist := launchDaemonPlist(cfg)
	if err := writeRootFile(cfg.PlistPath, []byte(plist), 0o644); err != nil {
		return fmt.Errorf("write launch daemon plist: %w", err)
	}

	if cfg.NoStart {
		_, _ = fmt.Fprintf(stdout, "Installed FastProxy daemon at %s\n", cfg.PlistPath)
		_, _ = fmt.Fprintln(stdout, "Start it later with: sudo launchctl bootstrap system "+cfg.PlistPath)
		return nil
	}

	_ = runLaunchctl("bootout", "system", cfg.PlistPath)
	if err := runLaunchctl("bootstrap", "system", cfg.PlistPath); err != nil {
		return fmt.Errorf("bootstrap launch daemon: %w", err)
	}
	if err := runLaunchctl("enable", "system/"+cfg.Label); err != nil {
		return fmt.Errorf("enable launch daemon: %w", err)
	}
	if err := runLaunchctl("kickstart", "-k", "system/"+cfg.Label); err != nil {
		return fmt.Errorf("start launch daemon: %w", err)
	}

	_, _ = fmt.Fprintf(stdout, "Installed and started FastProxy daemon: %s\n", cfg.Label)
	return nil
}

func parseInstallConfig(args []string, stderr io.Writer) (installConfig, error) {
	cfg := defaultInstallConfig()
	flags := flag.NewFlagSet("install", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&cfg.Addr, "addr", cfg.Addr, "HTTP listen address for the installed daemon")
	flags.StringVar(&cfg.DataDir, "data-dir", cfg.DataDir, "application data directory for the installed daemon")
	flags.StringVar(&cfg.HelperPath, "helper-path", cfg.HelperPath, "installed helper executable path")
	flags.StringVar(&cfg.Label, "label", cfg.Label, "launchd service label")
	flags.StringVar(&cfg.LogDir, "log-dir", cfg.LogDir, "daemon log directory")
	flags.StringVar(&cfg.LogLevel, "log-level", cfg.LogLevel, "daemon log level")
	flags.StringVar(&cfg.MihomoBinaryPath, "mihomo-bin", env("FASTPROXY_SERVER_MIHOMO_BIN", ""), "mihomo binary path for the installed daemon")
	flags.BoolVar(&cfg.NoStart, "no-start", false, "install files without loading the launch daemon")
	flags.StringVar(&cfg.PlistPath, "plist-path", cfg.PlistPath, "launch daemon plist path")
	flags.StringVar(&cfg.SingBoxBinaryPath, "sing-box-bin", env("FASTPROXY_SERVER_SING_BOX_BIN", ""), "sing-box binary path for the installed daemon")
	if err := flags.Parse(args); err != nil {
		return installConfig{}, err
	}
	if flags.NArg() != 0 {
		return installConfig{}, fmt.Errorf("unexpected install arguments: %v", flags.Args())
	}
	return cfg, nil
}

func copyExecutable(src string, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("stat current executable: %w", err)
	}
	if dstInfo, err := os.Stat(dst); err == nil && os.SameFile(srcInfo, dstInfo) {
		return nil
	}

	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open current executable: %w", err)
	}
	defer in.Close()

	tmp, err := os.CreateTemp(filepath.Dir(dst), "."+filepath.Base(dst)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary helper: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		return fmt.Errorf("copy helper executable: %w", err)
	}
	if err := tmp.Chmod(0o755); err != nil {
		tmp.Close()
		return fmt.Errorf("chmod helper executable: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close helper executable: %w", err)
	}
	if err := os.Rename(tmpName, dst); err != nil {
		return fmt.Errorf("install helper executable: %w", err)
	}
	if err := os.Chown(dst, 0, 0); err != nil {
		return fmt.Errorf("chown helper executable: %w", err)
	}
	return nil
}

func writeRootFile(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	if err := os.Chown(path, 0, 0); err != nil {
		return err
	}
	return nil
}

func launchDaemonPlist(cfg installConfig) string {
	args := []string{
		cfg.HelperPath,
		"serve",
		"--addr", cfg.Addr,
		"--data-dir", cfg.DataDir,
		"--log-level", cfg.LogLevel,
	}
	if cfg.MihomoBinaryPath != "" {
		args = append(args, "--mihomo-bin", cfg.MihomoBinaryPath)
	}
	if cfg.SingBoxBinaryPath != "" {
		args = append(args, "--sing-box-bin", cfg.SingBoxBinaryPath)
	}

	var buf bytes.Buffer
	buf.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	buf.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	buf.WriteString(`<plist version="1.0">` + "\n")
	buf.WriteString("<dict>\n")
	writePlistString(&buf, "Label", cfg.Label)
	buf.WriteString("\t<key>ProgramArguments</key>\n")
	buf.WriteString("\t<array>\n")
	for _, arg := range args {
		writePlistArrayString(&buf, arg)
	}
	buf.WriteString("\t</array>\n")
	writePlistBool(&buf, "RunAtLoad", true)
	writePlistBool(&buf, "KeepAlive", true)
	writePlistString(&buf, "WorkingDirectory", cfg.DataDir)
	writePlistString(&buf, "StandardOutPath", filepath.Join(cfg.LogDir, "fastproxy-server.log"))
	writePlistString(&buf, "StandardErrorPath", filepath.Join(cfg.LogDir, "fastproxy-server.err.log"))
	buf.WriteString("</dict>\n")
	buf.WriteString("</plist>\n")
	return buf.String()
}

func writePlistString(buf *bytes.Buffer, key string, value string) {
	buf.WriteString("\t<key>")
	writeXMLEscaped(buf, key)
	buf.WriteString("</key>\n\t<string>")
	writeXMLEscaped(buf, value)
	buf.WriteString("</string>\n")
}

func writePlistArrayString(buf *bytes.Buffer, value string) {
	buf.WriteString("\t\t<string>")
	writeXMLEscaped(buf, value)
	buf.WriteString("</string>\n")
}

func writePlistBool(buf *bytes.Buffer, key string, value bool) {
	buf.WriteString("\t<key>")
	writeXMLEscaped(buf, key)
	if value {
		buf.WriteString("</key>\n\t<true/>\n")
		return
	}
	buf.WriteString("</key>\n\t<false/>\n")
}

func writeXMLEscaped(buf *bytes.Buffer, value string) {
	for _, r := range value {
		switch r {
		case '&':
			buf.WriteString("&amp;")
		case '<':
			buf.WriteString("&lt;")
		case '>':
			buf.WriteString("&gt;")
		case '"':
			buf.WriteString("&quot;")
		case '\'':
			buf.WriteString("&apos;")
		default:
			buf.WriteRune(r)
		}
	}
}

func runLaunchctl(args ...string) error {
	cmd := exec.Command("launchctl", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("launchctl %v: %w: %s", args, err, bytes.TrimSpace(output))
	}
	return nil
}

func env(key string, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
