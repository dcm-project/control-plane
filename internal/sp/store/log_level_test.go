package store

import (
	"log/slog"
	"testing"

	"gorm.io/gorm/logger"
)

func TestGormLogLevelFromString(t *testing.T) {
	tests := []struct {
		level    string
		gormLvl  logger.LogLevel
		slogLvl  slog.Level
	}{
		{"debug", logger.Info, slog.LevelDebug},
		{"info", logger.Info, slog.LevelInfo},
		{"warn", logger.Warn, slog.LevelWarn},
		{"warning", logger.Warn, slog.LevelWarn},
		{"error", logger.Error, slog.LevelError},
		{"unknown", logger.Warn, slog.LevelWarn},
	}

	for _, tt := range tests {
		t.Run(tt.level, func(t *testing.T) {
			gotGorm, gotSlog := gormLogLevelFromString(tt.level)
			if gotGorm != tt.gormLvl {
				t.Fatalf("gorm level: got %v, want %v", gotGorm, tt.gormLvl)
			}
			if gotSlog != tt.slogLvl {
				t.Fatalf("slog level: got %v, want %v", gotSlog, tt.slogLvl)
			}
		})
	}
}
