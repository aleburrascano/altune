package config

import "testing"

func TestTestAuthEnabled_AllowlistFailsClosed(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want bool
	}{
		{"development enables", "development", true},
		{"test enables", "test", true},
		{"case-insensitive", "Development", true},
		{"trims padding", "  test  ", true},
		{"production disabled", "production", false},
		{"prod disabled", "prod", false},
		{"empty disabled (fail closed)", "", false},
		{"unknown disabled (fail closed)", "staging", false},
		{"misspelled disabled (fail closed)", "developmnt", false},
		{"substring not matched", "development-prod", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{Env: tt.env}
			if got := cfg.TestAuthEnabled(); got != tt.want {
				t.Errorf("TestAuthEnabled() with Env=%q = %v, want %v", tt.env, got, tt.want)
			}
		})
	}
}
