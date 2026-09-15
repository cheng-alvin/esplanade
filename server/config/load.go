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
	if v := os.Getenv("ESPLANADE_MONGO_URI"); v != "" {
		config.MongoURI = v
	}
	if v := os.Getenv("ESPLANADE_MONGO_DATABASE"); v != "" {
		config.MongoDatabase = v
	}
	if v := os.Getenv("ESPLANADE_MONGO_CONNECT_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("invalid ESPLANADE_MONGO_CONNECT_TIMEOUT %q: %w", v, err)
		}
		config.MongoConnectTimeout = d
	}
	if v := os.Getenv("ESPLANADE_MONGO_SERVER_SELECTION_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("invalid ESPLANADE_MONGO_SERVER_SELECTION_TIMEOUT %q: %w", v, err)
		}
		config.MongoServerSelectionTimeout = d
	}
	if v := os.Getenv("ESPLANADE_MONGO_MAX_POOL_SIZE"); v != "" {
		p, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid ESPLANADE_MONGO_MAX_POOL_SIZE %q: %w", v, err)
		}
		config.MongoMaxPoolSize = p
	}
	if v := os.Getenv("ESPLANADE_MONGO_MIN_POOL_SIZE"); v != "" {
		p, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid ESPLANADE_MONGO_MIN_POOL_SIZE %q: %w", v, err)
		}
		config.MongoMinPoolSize = p
	}

	return config, nil
}
