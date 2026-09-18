package redis

import (
	"bytes"
	"context"
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
			var buf bytes.Buffer
			prev := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
			defer slog.SetDefault(prev)

			if client := NewClient(context.Background(), rawURL, 0); client != nil {
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

// The pool ceiling must come from configuration, never from GOMAXPROCS
// (go-redis's own default), and an unusable value must land on the documented
// default. The address is unreachable on purpose: NewClient returns the client
// anyway, so the ceiling is observable without a live redis.
func TestNewClient_PinsPoolSizeFromConfiguration(t *testing.T) {
	cases := []struct {
		name     string
		poolSize int
		want     int
	}{
		{name: "configured", poolSize: 33, want: 33},
		{name: "zero falls back", poolSize: 0, want: defaultPoolSize},
		{name: "negative falls back", poolSize: -1, want: defaultPoolSize},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := NewClient(context.Background(), "redis://127.0.0.1:1/0", tc.poolSize)
			if client == nil {
				t.Fatal("expected a client for a parseable URL")
			}
			t.Cleanup(func() { _ = client.Close() })

			if got := ReadPoolStats(client).PoolSize; got != tc.want {
				t.Fatalf("PoolSize = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestReadPoolStats_NilClientReadsAsZero(t *testing.T) {
	if stats := ReadPoolStats(nil); stats != (PoolStats{}) {
		t.Fatalf("ReadPoolStats(nil) = %+v, want zero value", stats)
	}
}
