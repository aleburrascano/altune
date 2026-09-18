package config

import "testing"

// TestLoad_PoolSizes guards the connection-pool knobs (#1610): both default to
// a ceiling derived from this service's own concurrency rather than from the
// host's CPU count, and an operator can tune either from the environment.
func TestLoad_PoolSizes(t *testing.T) {
	cases := []struct {
		name          string
		env           map[string]string
		wantDBMax     int
		wantRedisSize int
	}{
		{name: "defaults", env: nil, wantDBMax: 20, wantRedisSize: 50},
		{
			name:          "tuned",
			env:           map[string]string{"DB_POOL_MAX_CONNS": "40", "REDIS_POOL_SIZE": "80"},
			wantDBMax:     40,
			wantRedisSize: 80,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := feedbackBaseEnv()
			for k, v := range tc.env {
				env[k] = v
			}
			setEnv(t, env)

			cfg, err := Load()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cfg.DBPoolMaxConns != tc.wantDBMax {
				t.Errorf("DBPoolMaxConns = %d, want %d", cfg.DBPoolMaxConns, tc.wantDBMax)
			}
			if cfg.RedisPoolSize != tc.wantRedisSize {
				t.Errorf("RedisPoolSize = %d, want %d", cfg.RedisPoolSize, tc.wantRedisSize)
			}
		})
	}
}
