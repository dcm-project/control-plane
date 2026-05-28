package config

import (
	"os"
	"testing"
	"time"
)

func TestLoad_pgsqlRequiresCredentials(t *testing.T) {
	t.Setenv("DB_TYPE", "pgsql")
	t.Setenv("DB_USER", "")
	t.Setenv("DB_PASSWORD", "")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error when pgsql credentials are missing")
	}
}

func TestLoad_pgsqlWithCredentials(t *testing.T) {
	t.Setenv("DB_TYPE", "pgsql")
	t.Setenv("DB_USER", "test_user")
	t.Setenv("DB_PASSWORD", "test_password")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Database.User != "test_user" {
		t.Fatalf("DB_USER: got %q", cfg.Database.User)
	}
}

func TestLoad_placementTimeoutDefault(t *testing.T) {
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "/tmp/catalog-placement-timeout.db")
	t.Setenv("DB_USER", "")
	t.Setenv("DB_PASSWORD", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Placement.Timeout != 10*time.Second {
		t.Fatalf("PLACEMENT_MANAGER_TIMEOUT: got %v", cfg.Placement.Timeout)
	}

	_ = os.Remove("/tmp/catalog-placement-timeout.db")
}

func TestLoad_httpServerTimeoutDefaults(t *testing.T) {
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "/tmp/catalog-test.db")
	t.Setenv("DB_USER", "")
	t.Setenv("DB_PASSWORD", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Service.ReadHeaderTimeout != 10*time.Second {
		t.Fatalf("READ_HEADER_TIMEOUT: got %v", cfg.Service.ReadHeaderTimeout)
	}
	if cfg.Service.ReadTimeout != 15*time.Second {
		t.Fatalf("READ_TIMEOUT: got %v", cfg.Service.ReadTimeout)
	}
	if cfg.Service.WriteTimeout != 15*time.Second {
		t.Fatalf("WRITE_TIMEOUT: got %v", cfg.Service.WriteTimeout)
	}
	if cfg.Service.IdleTimeout != 60*time.Second {
		t.Fatalf("IDLE_TIMEOUT: got %v", cfg.Service.IdleTimeout)
	}
	if cfg.Service.MaxHeaderBytes != 1<<20 {
		t.Fatalf("MAX_HEADER_BYTES: got %d", cfg.Service.MaxHeaderBytes)
	}
}

func TestNormalizeDBType_postgresSynonyms(t *testing.T) {
	for _, raw := range []string{"pgsql", "postgres", "POSTGRESQL"} {
		got, err := NormalizeDBType(raw)
		if err != nil {
			t.Fatalf("NormalizeDBType(%q): %v", raw, err)
		}
		if got != "pgsql" {
			t.Fatalf("NormalizeDBType(%q): got %q", raw, got)
		}
	}
}

func TestLoad_rejectsUnsupportedDBType(t *testing.T) {
	t.Setenv("DB_TYPE", "mysql")
	t.Setenv("DB_USER", "u")
	t.Setenv("DB_PASSWORD", "p")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for unsupported DB_TYPE")
	}
}

func TestLoad_postgresRequiresCredentials(t *testing.T) {
	t.Setenv("DB_TYPE", "postgresql")
	t.Setenv("DB_USER", "")
	t.Setenv("DB_PASSWORD", "")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error when postgres credentials are missing")
	}
}

func TestLoad_sqliteDoesNotRequireCredentials(t *testing.T) {
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "/tmp/catalog-test.db")
	t.Setenv("DB_USER", "")
	t.Setenv("DB_PASSWORD", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Database.Type != "sqlite" {
		t.Fatalf("DB_TYPE: got %q", cfg.Database.Type)
	}

	_ = os.Remove("/tmp/catalog-test.db")
}
