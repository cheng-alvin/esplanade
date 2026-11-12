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

	JWTSecret string `yaml:"jwt_secret" env:"JWT_SECRET"`

	AccessTokenTTL        time.Duration `yaml:"access_token_ttl" env:"ACCESS_TOKEN_TTL"`
	RefreshTokenTTL       time.Duration `yaml:"refresh_token_ttl" env:"REFRESH_TOKEN_TTL"`
	PurgeRetention        time.Duration `yaml:"purge_retention" env:"PURGE_RETENTION"`
	VerificationTokenTTL  time.Duration `yaml:"verification_token_ttl" env:"VERIFICATION_TOKEN_TTL"`
	PasswordResetTokenTTL time.Duration `yaml:"password_reset_token_ttl" env:"PASSWORD_RESET_TOKEN_TTL"`

	AppBaseURL     string `yaml:"app_base_url" env:"APP_BASE_URL"`
	GoogleClientID string `yaml:"google_client_id" env:"GOOGLE_CLIENT_ID"`

	SMTPHost     string `yaml:"smtp_host" env:"SMTP_HOST"`
	SMTPPort     int    `yaml:"smtp_port" env:"SMTP_PORT"`
	SMTPUsername string `yaml:"smtp_username" env:"SMTP_USERNAME"`
	SMTPPassword string `yaml:"smtp_password" env:"SMTP_PASSWORD"`
	SMTPFrom     string `yaml:"smtp_from" env:"SMTP_FROM"`

	ResendLimit  int           `yaml:"resend_limit" env:"RESEND_LIMIT"`
	ResendWindow time.Duration `yaml:"resend_window" env:"RESEND_WINDOW"`
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

		AccessTokenTTL:        15 * time.Minute,
		RefreshTokenTTL:       720 * time.Hour,
		PurgeRetention:        720 * time.Hour,
		VerificationTokenTTL:  24 * time.Hour,
		PasswordResetTokenTTL: time.Hour,

		SMTPPort: 587,

		ResendLimit:  3,
		ResendWindow: time.Hour,
	}
}
