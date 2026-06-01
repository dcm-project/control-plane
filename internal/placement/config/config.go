// Package config handles loading and validating service configuration.
package config

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kelseyhightower/envconfig"
)

type Config struct {
	Service          *ServiceConfig
	Database         *DBConfig
	PolicyEvaluation *PolicyManagerEvaluationConfig
	SPRM             *SPRMConfig
}

type ServiceConfig struct {
	Address  string `envconfig:"SVC_ADDRESS" default:":8080"`
	LogLevel string `envconfig:"SVC_LOG_LEVEL" default:"info"`
}

// DBConfig holds database configuration
type DBConfig struct {
	Type     string `envconfig:"DB_TYPE" default:"pgsql"`
	Hostname string `envconfig:"DB_HOST" default:"localhost"`
	Port     string `envconfig:"DB_PORT" default:"5432"`
	Name     string `envconfig:"DB_NAME" default:"placement-manager"`
	User     string `envconfig:"DB_USER"`
	Password string `envconfig:"DB_PASSWORD"`
}

// PolicyManagerEvaluationConfig holds policy manager configuration
type PolicyManagerEvaluationConfig struct {
	URL     string        `envconfig:"POLICY_MANAGER_EVALUATION_URL" default:"http://localhost:8081"`
	Timeout time.Duration `envconfig:"POLICY_MANAGER_EVALUATION_TIMEOUT" default:"10s"`
}

// SPRMConfig holds service provider resource manager configuration
type SPRMConfig struct {
	URL     string        `envconfig:"SP_RESOURCE_MANAGER_URL" default:"http://localhost:8082"`
	Timeout time.Duration `envconfig:"SP_RESOURCE_MANAGER_TIMEOUT" default:"10s"`
}

// Load reads configuration from environment variables
func Load() (*Config, error) {
	cfg := &Config{}
	if err := envconfig.Process("", cfg); err != nil {
		return nil, err
	}
	if err := validate(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
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
