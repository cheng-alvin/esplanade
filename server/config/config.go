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
	"time"
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
