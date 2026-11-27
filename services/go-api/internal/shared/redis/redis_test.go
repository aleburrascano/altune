package redis_test

import (
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/redis"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestNewClient_InvalidURLDoesNotLogCredentials(t *testing.T) {
	cases := map[string]string{
		"invalid host":   "redis://admin:hunter2secret@local host:6379/0",
		"invalid escape": "redis://admin:hunter2secret@localhost:6379/%zz",
		"control char":   "redis://admin:hunter2secret@localhost:6379/\x7f",
	}

	for name, rawURL := range cases {
		t.Run(name, func(t *testing.T) {
			buf := logging.CaptureText(t, slog.LevelDebug)

			if client := redis.NewClient(context.Background(), rawURL, 0); client != nil {
				t.Fatalf("expected nil client for unparseable URL")
			}

			out := buf.String()
			if !strings.Contains(out, "invalid redis URL") {
				t.Fatalf("expected invalid-URL warning, got %q", out)
			}
			for _, secret := range []string{"hunter2secret", "admin:", rawURL} {
				if strings.Contains(out, secret) {
					t.Errorf("log leaks %q: %s", secret, out)
				}
			}
		})
	}
}

func TestNewClient_PinsPoolSizeFromConfiguration(t *testing.T) {
	cases := []struct {
		name     string
		poolSize int
		want     int
	}{
		{name: "configured", poolSize: 33, want: 33},
		{name: "zero falls back", poolSize: 0, want: 50},
		{name: "negative falls back", poolSize: -1, want: 50},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := redis.NewClient(context.Background(), "redis://127.0.0.1:1/0", tc.poolSize)
			if client == nil {
				t.Fatal("expected a client for a parseable URL")
			}
			t.Cleanup(func() { _ = client.Close() })

			if got := redis.ReadPoolStats(client).PoolSize; got != tc.want {
				t.Fatalf("PoolSize = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestReadPoolStats_NilClientReadsAsZero(t *testing.T) {
	got, err := json.Marshal(redis.ReadPoolStats(nil))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	const want = `{"pool_size":0,"total_conns":0,"idle_conns":0,"timeouts":0}`
	if string(got) != want {
		t.Fatalf("ReadPoolStats(nil) = %s, want %s", got, want)
	}
}
