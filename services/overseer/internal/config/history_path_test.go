package config_test

import (
	"altune/overseer/internal/config"
	"testing"
)

func TestHistoryPathDefaultsToTheOverseerVolume(t *testing.T) {
	setEnv(t, validEnv())
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.HistoryPath != "/var/lib/overseer/history.db" {
		t.Fatalf("HistoryPath = %q, want the overseer-data volume file", cfg.HistoryPath)
	}
}

func TestHistoryPathReadsTheEnvironment(t *testing.T) {
	env := validEnv()
	env["OVERSEER_HISTORY_PATH"] = "  /tmp/h.db "
	setEnv(t, env)
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.HistoryPath != "/tmp/h.db" {
		t.Fatalf("HistoryPath = %q, want /tmp/h.db", cfg.HistoryPath)
	}
}
