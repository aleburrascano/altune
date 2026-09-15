package config

import (
	"os"
	"testing"
)

// TestLoad_SSEMaxConns guards the SSE_MAX_CONNS knob (#1022): it defaults to a
// bounded global ceiling and an operator can tune it from the environment.
func TestLoad_SSEMaxConns(t *testing.T) {
	cases := []struct {
		name string
		env  string
		want int
	}{
		{name: "default", env: "", want: 2048},
		{name: "tuned", env: "500", want: 500},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("SSE_MAX_CONNS", "")
			if err := os.Unsetenv("SSE_MAX_CONNS"); err != nil {
				t.Fatal(err)
			}
			env := feedbackBaseEnv()
			if tc.env != "" {
				env["SSE_MAX_CONNS"] = tc.env
			}
			setEnv(t, env)

			cfg, err := Load()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cfg.SSEMaxConns != tc.want {
				t.Fatalf("SSEMaxConns = %d, want %d", cfg.SSEMaxConns, tc.want)
			}
		})
	}
}
