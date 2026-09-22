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
	Env  string `yaml:"env" env:"ENV"`
	Host string `yaml:"host" env:"HOST"`

	Port int `yaml:"port" env:"PORT"`

	LogLevel        string        `yaml:"log_level" env:"LOG_LEVEL"`
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout" env:"SHUTDOWN_TIMEOUT"`
	CORSOrigins     []string      `yaml:"cors_origins" env:"CORS_ORIGINS" envSeparator:","`

	MongoURI                    string        `yaml:"mongo_uri" env:"MONGO_URI"`
	MongoDatabase               string        `yaml:"mongo_database" env:"MONGO_DATABASE"`
	MongoConnectTimeout         time.Duration `yaml:"mongo_connect_timeout" env:"MONGO_CONNECT_TIMEOUT"`
	MongoServerSelectionTimeout time.Duration `yaml:"mongo_server_selection_timeout" env:"MONGO_SERVER_SELECTION_TIMEOUT"`
	MongoMaxPoolSize            uint64        `yaml:"mongo_max_pool_size" env:"MONGO_MAX_POOL_SIZE"`
	MongoMinPoolSize            uint64        `yaml:"mongo_min_pool_size" env:"MONGO_MIN_POOL_SIZE"`
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

		MongoDatabase:               "esplanade",
		MongoConnectTimeout:         10 * time.Second,
		MongoServerSelectionTimeout: 10 * time.Second,
		MongoMaxPoolSize:            100,
		MongoMinPoolSize:            0,
	}
}
