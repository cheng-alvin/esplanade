package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

func Load() (*Config, error) {
	config := defaults()

	if data, err := os.ReadFile("config.yaml"); err == nil {
		if err := yaml.Unmarshal(data, config); err != nil {
			return nil, fmt.Errorf("parsing config.yaml: %w", err)
		}
	}

	if v := os.Getenv("ESPLANADE_ENV"); v != "" {
		config.Env = v
	}
	if v := os.Getenv("ESPLANADE_HOST"); v != "" {
		config.Host = v
	}
	if v := os.Getenv("ESPLANADE_PORT"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("invalid ESPLANADE_PORT %q: %w", v, err)
		}
		config.Port = p
	}
	if v := os.Getenv("ESPLANADE_LOG_LEVEL"); v != "" {
		config.LogLevel = v
	}
	if v := os.Getenv("ESPLANADE_SHUTDOWN_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("invalid ESPLANADE_SHUTDOWN_TIMEOUT %q: %w", v, err)
		}
		config.ShutdownTimeout = d
	}
	if v := os.Getenv("ESPLANADE_CORS_ORIGINS"); v != "" {
		config.CORSOrigins = strings.Split(v, ",")
	}

	return config, nil
}
