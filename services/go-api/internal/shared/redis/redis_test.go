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

			if client := NewClient(context.Background(), rawURL); client != nil {
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
