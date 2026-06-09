// Package config provides application configuration loaded from environment variables.
package config

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kelseyhightower/envconfig"
)

// ServiceConfig holds HTTP server configuration
type ServiceConfig struct {
	BindAddress       string        `envconfig:"BIND_ADDRESS" default:"127.0.0.1:8080"`
	LogLevel          string        `envconfig:"LOG_LEVEL" default:"info"`
	ReadHeaderTimeout time.Duration `envconfig:"READ_HEADER_TIMEOUT" default:"10s"`
	ReadTimeout       time.Duration `envconfig:"READ_TIMEOUT" default:"15s"`
	WriteTimeout      time.Duration `envconfig:"WRITE_TIMEOUT" default:"15s"`
	IdleTimeout       time.Duration `envconfig:"IDLE_TIMEOUT" default:"60s"`
	MaxHeaderBytes    int           `envconfig:"MAX_HEADER_BYTES" default:"1048576"`
}

// DBConfig holds database configuration
type DBConfig struct {
	Type     string `envconfig:"DB_TYPE" default:"pgsql"`
	Hostname string `envconfig:"DB_HOST" default:"localhost"`
	Port     string `envconfig:"DB_PORT" default:"5432"`
	Name     string `envconfig:"DB_NAME" default:"catalog-manager"`
	User     string `envconfig:"DB_USER"`
	Password string `envconfig:"DB_PASSWORD"`
}

// SeedConfig holds configuration for seeding default catalog items
type SeedConfig struct {
	// RegionDefault for metadata.labels.region; empty leaves the field without a default.
	RegionDefault string   `envconfig:"SEED_REGION_DEFAULT" default:""`
	RegionEnum    []string `envconfig:"SEED_REGION_ENUM" default:"region-a,region-b"`
}

func DefaultSeedConfig() SeedConfig {
	return SeedConfig{
		RegionEnum: []string{"region-a", "region-b"},
	}
}

// Config holds all configuration for the application
type Config struct {
	Service  ServiceConfig
	Database DBConfig
	Seed     SeedConfig
}

func Load() (*Config, error) {
	var cfg Config
	if err := envconfig.Process("", &cfg.Service); err != nil {
		return nil, err
	}
	if err := envconfig.Process("", &cfg.Database); err != nil {
		return nil, err
	}
	if err := envconfig.Process("", &cfg.Seed); err != nil {
		return nil, err
	}
	if err := validate(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// NormalizeDBType maps DB_TYPE values to a supported backend name.
func NormalizeDBType(dbType string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(dbType)) {
	case "pgsql", "postgres", "postgresql":
		return "pgsql", nil
	case "sqlite":
		return "sqlite", nil
	default:
		return "", fmt.Errorf("unsupported DB_TYPE %q (supported: pgsql, sqlite)", dbType)
	}
}

func validate(cfg *Config) error {
	dbType, err := NormalizeDBType(cfg.Database.Type)
	if err != nil {
		return err
	}
	cfg.Database.Type = dbType

	if dbType != "pgsql" {
		return nil
	}
	if cfg.Database.User == "" {
		return errors.New("DB_USER is required when DB_TYPE is pgsql")
	}
	if cfg.Database.Password == "" {
		return errors.New("DB_PASSWORD is required when DB_TYPE is pgsql")
	}
	return nil
}
