package store

import (
	"log/slog"
	"testing"

	"github.com/dcm-project/control-plane/internal/catalog/config"
)

func TestInitDB_unsupportedDBType(t *testing.T) {
	cfg := &config.Config{
		Database: config.DBConfig{Type: "mysql", Name: "catalog"},
	}

	_, err := InitDB(cfg, slog.Default())
	if err == nil {
		t.Fatal("expected error for unsupported DB_TYPE")
	}
}
