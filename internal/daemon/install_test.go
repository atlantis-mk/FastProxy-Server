package daemon

import (
	"bytes"
	"strings"
	"testing"
)

func TestParseInstallConfig(t *testing.T) {
	var stderr bytes.Buffer
	cfg, err := parseInstallConfig([]string{
		"--addr", "127.0.0.1:5000",
		"--data-dir", "/tmp/fastproxy data",
		"--helper-path", "/tmp/fastproxy",
		"--label", "com.example.fastproxy",
		"--log-dir", "/tmp/logs",
		"--log-level", "debug",
		"--mihomo-bin", "/tmp/mihomo",
		"--sing-box-bin", "/tmp/sing-box",
		"--plist-path", "/tmp/com.example.fastproxy.plist",
		"--no-start",
	}, &stderr)
	if err != nil {
		t.Fatalf("parseInstallConfig returned error: %v", err)
	}

	if cfg.Addr != "127.0.0.1:5000" {
		t.Fatalf("Addr = %q", cfg.Addr)
	}
	if cfg.DataDir != "/tmp/fastproxy data" {
		t.Fatalf("DataDir = %q", cfg.DataDir)
	}
	if cfg.HelperPath != "/tmp/fastproxy" {
		t.Fatalf("HelperPath = %q", cfg.HelperPath)
	}
	if cfg.Label != "com.example.fastproxy" {
		t.Fatalf("Label = %q", cfg.Label)
	}
	if cfg.LogDir != "/tmp/logs" {
		t.Fatalf("LogDir = %q", cfg.LogDir)
	}
	if cfg.LogLevel != "debug" {
		t.Fatalf("LogLevel = %q", cfg.LogLevel)
	}
	if cfg.MihomoBinaryPath != "/tmp/mihomo" {
		t.Fatalf("MihomoBinaryPath = %q", cfg.MihomoBinaryPath)
	}
	if cfg.SingBoxBinaryPath != "/tmp/sing-box" {
		t.Fatalf("SingBoxBinaryPath = %q", cfg.SingBoxBinaryPath)
	}
	if cfg.PlistPath != "/tmp/com.example.fastproxy.plist" {
		t.Fatalf("PlistPath = %q", cfg.PlistPath)
	}
	if !cfg.NoStart {
		t.Fatal("NoStart = false")
	}
}

func TestLaunchDaemonPlist(t *testing.T) {
	cfg := installConfig{
		Addr:              "127.0.0.1:43171",
		DataDir:           "/Library/Application Support/FastProxy",
		HelperPath:        "/Library/PrivilegedHelperTools/fastproxy-server",
		Label:             "com.fastproxy.server",
		LogDir:            "/Library/Logs/FastProxy",
		LogLevel:          "info",
		MihomoBinaryPath:  "/opt/core/mihomo & beta",
		SingBoxBinaryPath: "/opt/core/sing-box",
	}

	plist := launchDaemonPlist(cfg)
	for _, want := range []string{
		"<key>Label</key>",
		"<string>com.fastproxy.server</string>",
		"<string>/Library/PrivilegedHelperTools/fastproxy-server</string>",
		"<string>serve</string>",
		"<string>--data-dir</string>",
		"<string>/Library/Application Support/FastProxy</string>",
		"<string>--mihomo-bin</string>",
		"<string>/opt/core/mihomo &amp; beta</string>",
		"<key>RunAtLoad</key>",
		"<true/>",
		"<key>KeepAlive</key>",
		"<string>/Library/Logs/FastProxy/fastproxy-server.log</string>",
	} {
		if !strings.Contains(plist, want) {
			t.Fatalf("plist does not contain %q:\n%s", want, plist)
		}
	}
}
