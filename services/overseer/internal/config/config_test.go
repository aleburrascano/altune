package config_test

import (
	"altune/overseer/internal/config"
	"strings"
	"testing"
)

func setEnv(t *testing.T, kv map[string]string) {
	t.Helper()
	for k, v := range kv {
		t.Setenv(k, v)
	}
}

const goodToken = "0123456789abcdef0123456789abcdef" // 32 chars

func TestLoadRejectsMissingOwnerToken(t *testing.T) {
	setEnv(t, map[string]string{"OVERSEER_OWNER_TOKEN": ""})
	if _, err := config.Load(); err == nil {
		t.Fatal("expected error with no owner token, got nil")
	}
}

func TestLoadRejectsShortOwnerToken(t *testing.T) {
	setEnv(t, map[string]string{"OVERSEER_OWNER_TOKEN": "tooshort"})
	if _, err := config.Load(); err == nil {
		t.Fatal("expected error with short owner token, got nil")
	}
}

func TestLoadDefaults(t *testing.T) {
	setEnv(t, map[string]string{"OVERSEER_OWNER_TOKEN": goodToken})
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Port != 8090 {
		t.Errorf("Port = %d, want default 8090", cfg.Port)
	}
	if !cfg.IsDevelopment() {
		t.Error("expected development env by default")
	}
	if cfg.TickInterval <= 0 {
		t.Errorf("TickInterval = %v, want positive default", cfg.TickInterval)
	}
}

func TestLoadRejectsBadPort(t *testing.T) {
	setEnv(t, map[string]string{"OVERSEER_OWNER_TOKEN": goodToken, "OVERSEER_PORT": "70000"})
	if _, err := config.Load(); err == nil {
		t.Fatal("expected error for out-of-range port")
	}
}

func TestLoadRejectsBadTick(t *testing.T) {
	setEnv(t, map[string]string{"OVERSEER_OWNER_TOKEN": goodToken, "OVERSEER_TICK_INTERVAL": "-1s"})
	if _, err := config.Load(); err == nil {
		t.Fatal("expected error for non-positive tick interval")
	}
}

func TestLogValueRedactsToken(t *testing.T) {
	setEnv(t, map[string]string{"OVERSEER_OWNER_TOKEN": goodToken})
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.LogValue().String(); strings.Contains(got, goodToken) {
		t.Errorf("LogValue leaked the owner token: %s", got)
	}
}
