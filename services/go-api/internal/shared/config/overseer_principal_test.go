package config

import (
	"strings"
	"testing"
)

const hexOverseerID = "cccccccc-dddd-4eee-8fff-000000000000"

func loadWithOverseerPrincipal(t *testing.T, raw string) (*Config, error) {
	t.Helper()
	setEnv(t, validConfigEnv(map[string]string{"OPERATOR_USER_ID": hexOperatorID}))
	t.Setenv("OVERSEER_PRINCIPAL_ID", raw)
	return Load()
}

func TestLoad_OverseerPrincipalIsCanonical(t *testing.T) {
	cfg, err := loadWithOverseerPrincipal(t, "  "+strings.ToUpper(hexOverseerID)+" ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.OverseerPrincipalID != hexOverseerID {
		t.Errorf("OverseerPrincipalID = %q, want %q", cfg.OverseerPrincipalID, hexOverseerID)
	}
}

func TestLoad_OverseerPrincipalIsOptional(t *testing.T) {
	for _, raw := range []string{"", "   "} {
		cfg, err := loadWithOverseerPrincipal(t, raw)
		if err != nil {
			t.Fatalf("OVERSEER_PRINCIPAL_ID=%q: unexpected error: %v", raw, err)
		}
		if cfg.OverseerPrincipalID != "" {
			t.Errorf("OVERSEER_PRINCIPAL_ID=%q: got %q, want empty", raw, cfg.OverseerPrincipalID)
		}
	}
}

func TestLoad_OverseerPrincipalMustBeAUUID(t *testing.T) {
	_, err := loadWithOverseerPrincipal(t, "overseer")
	if err == nil || !strings.Contains(err.Error(), "OVERSEER_PRINCIPAL_ID") {
		t.Fatalf("err = %v, want an error naming OVERSEER_PRINCIPAL_ID", err)
	}
}

func TestLoad_OverseerPrincipalMustNotBeTheOperator(t *testing.T) {
	_, err := loadWithOverseerPrincipal(t, strings.ToUpper(hexOperatorID))
	if err == nil || !strings.Contains(err.Error(), "OVERSEER_PRINCIPAL_ID") {
		t.Fatalf("err = %v, want an error naming OVERSEER_PRINCIPAL_ID", err)
	}
}
