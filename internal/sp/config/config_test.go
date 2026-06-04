package config

import (
	"testing"
)

func TestLoad_pgsqlRequiresCredentials(t *testing.T) {
	t.Setenv("DB_TYPE", "pgsql")
	t.Setenv("DB_USER", "")
	t.Setenv("DB_PASS", "")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error when pgsql credentials are missing")
	}
}

func TestLoad_pgsqlWithCredentials(t *testing.T) {
	t.Setenv("DB_TYPE", "pgsql")
	t.Setenv("DB_USER", "test_user")
	t.Setenv("DB_PASS", "test_password")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Database.User != "test_user" {
		t.Fatalf("DB_USER: got %q", cfg.Database.User)
	}
	if cfg.Database.Password != "test_password" {
		t.Fatalf("DB_PASS: got %q", cfg.Database.Password)
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
	t.Setenv("DB_PASS", "p")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for unsupported DB_TYPE")
	}
}

func TestLoad_postgresRequiresCredentials(t *testing.T) {
	t.Setenv("DB_TYPE", "postgresql")
	t.Setenv("DB_USER", "")
	t.Setenv("DB_PASS", "")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error when postgres credentials are missing")
	}
}

func TestLoad_sqliteDoesNotRequireCredentials(t *testing.T) {
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "/tmp/sp-config-test.db")
	t.Setenv("DB_USER", "")
	t.Setenv("DB_PASS", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Database.Type != "sqlite" {
		t.Fatalf("DB_TYPE: got %q", cfg.Database.Type)
	}
}
