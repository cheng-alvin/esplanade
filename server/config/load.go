package config

import (
	"fmt"
	"os"

	"github.com/caarlos0/env/v11"
	"gopkg.in/yaml.v3"
)

func Load() (*Config, error) {
	config := defaults()

	if data, err := os.ReadFile("config.yaml"); err == nil {
		if err := yaml.Unmarshal(data, config); err != nil {
			return nil, fmt.Errorf("parsing config.yaml: %w", err)
		}
	}

	// Environmental variable overrides - configuration values prescribed
	// above by the unmarshalled YAML data can then be overridden and set
	// by local environment variables.

	// Note - Environment variables are prefixed with `ESPLANADE_`
	
	if err := env.ParseWithOptions(config, env.Options{
		Prefix: "ESPLANADE_",
	}); err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
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
