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

	return config, nil
}
