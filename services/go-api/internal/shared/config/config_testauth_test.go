package config

import "testing"

// TestAuthEnabled must require BOTH an explicit opt-in (TEST_AUTH_ENABLED=true)
// AND a non-prod ENV, and fail closed on any absent/ambiguous input.
func TestTestAuthEnabled_RequiresOptInAndNonProdEnv(t *testing.T) {
	tests := []struct {
		name  string
		env   string
		optIn bool
		want  bool
	}{
		// Only opt-in + non-prod env enables the backdoor.
		{"optin + development enables", "development", true, true},
		{"optin + test enables", "test", true, true},
		{"optin + case-insensitive env", "Development", true, true},
		{"optin + trims padding", "  test  ", true, true},

		// Opt-in alone in a prod-like or unknown env stays DISABLED.
		{"optin + production disabled", "production", true, false},
		{"optin + prod disabled", "prod", true, false},
		{"optin + unknown env disabled", "staging", true, false},
		{"optin + misspelled env disabled", "developmnt", true, false},
		{"optin + substring not matched", "development-prod", true, false},
		{"optin + empty env disabled (fail closed)", "", true, false},

		// Non-prod env WITHOUT the explicit opt-in stays DISABLED (fail closed):
		// this is the #1384 case — ENV defaults to development, so ENV alone must
		// never be sufficient.
		{"no optin + development disabled", "development", false, false},
		{"no optin + test disabled", "test", false, false},

		// Unset ENV and no opt-in — the exact prod-misconfig shape — DISABLED.
		{"unset env + no optin disabled (fail closed)", "", false, false},
		{"production + no optin disabled", "production", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{Env: tt.env, TestAuthOptIn: tt.optIn}
			if got := cfg.TestAuthEnabled(); got != tt.want {
				t.Errorf("TestAuthEnabled() with Env=%q optIn=%v = %v, want %v",
					tt.env, tt.optIn, got, tt.want)
			}
		})
	}
}

// The zero-value Config (ENV unset, opt-in unset) — the shape a prod deploy that
// forgets to set anything lands in — must resolve to DISABLED.
func TestTestAuthEnabled_ZeroValueFailsClosed(t *testing.T) {
	if (&Config{}).TestAuthEnabled() {
		t.Fatal("zero-value Config enabled test auth; must fail closed to DISABLED")
	}
}
