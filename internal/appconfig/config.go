package appconfig

import (
	"flag"
	"os"
)

type Config struct {
	Addr              string `json:"addr"`
	DataDir           string `json:"dataDir"`
	LogLevel          string `json:"logLevel"`
	MihomoBinaryPath  string `json:"mihomoBinaryPath"`
	SingBoxBinaryPath string `json:"singBoxBinaryPath"`
}

func Load() Config {
	cfg := Config{
		Addr:     env("FASTPROXY_SERVER_ADDR", "127.0.0.1:43171"),
		LogLevel: env("FASTPROXY_SERVER_LOG_LEVEL", "info"),
	}

	flag.StringVar(&cfg.Addr, "addr", cfg.Addr, "HTTP listen address")
	flag.StringVar(&cfg.DataDir, "data-dir", env("FASTPROXY_SERVER_DATA_DIR", ""), "application data directory")
	flag.StringVar(&cfg.LogLevel, "log-level", cfg.LogLevel, "log level")
	flag.StringVar(&cfg.MihomoBinaryPath, "mihomo-bin", env("FASTPROXY_SERVER_MIHOMO_BIN", ""), "mihomo binary path")
	flag.StringVar(&cfg.SingBoxBinaryPath, "sing-box-bin", env("FASTPROXY_SERVER_SING_BOX_BIN", ""), "sing-box binary path")
	flag.Parse()

	return cfg
}

func env(key string, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
