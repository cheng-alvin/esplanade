// Package config provides application configuration loading from
// environment variables and YAML files.
//
// Configuration precedence (highest → lowest):
//  1. Environment variables (prefixed with ESPLANADE_)
//  2. config.yaml in working directory
//  3. Built-in defaults
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Env  string `yaml:"env"`
	Host string `yaml:"host"`

	Port int `yaml:"port"`

	LogLevel        string        `yaml:"log_level"`
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout"`
	CORSOrigins     []string      `yaml:"cors_origins"`
}

func (c *Config) Addr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

func defaults() *Config {
	return &Config{
		Env:             "production",
		Host:            "0.0.0.0",
		Port:            8080,
		LogLevel:        "info",
		ShutdownTimeout: 15 * time.Second,
		CORSOrigins:     []string{},
	}
}

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
