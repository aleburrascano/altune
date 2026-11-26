package app_test

import (
	"altune/go-api/internal/app"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/dbtest"
	"bytes"
	"context"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

func runUntilItReturns(t *testing.T, cfg *config.Config) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	return app.New(cfg, nil).Run(ctx)
}

func TestRun_AnUnreachableDatabaseStopsStartup(t *testing.T) {
	cfg := jwksConfig(t)
	cfg.DatabaseURL = "postgres://nobody@127.0.0.1:1/none?connect_timeout=1"

	err := runUntilItReturns(t, cfg)

	if err == nil || !strings.HasPrefix(err.Error(), "database: ") {
		t.Fatalf("Run = %v, want the database error", err)
	}
}

func TestRun_AFailedAssemblyStopsStartup(t *testing.T) {
	dbtest.RequireIntegration(t)
	cfg := jwksConfig(t)
	cfg.DatabaseURL = dbtest.DatabaseURL(t)
	cfg.SupabaseJWTJWKSURL = "http://keys.example.com/jwks.json"

	err := runUntilItReturns(t, cfg)

	if err == nil || !strings.HasPrefix(err.Error(), "auth: ") {
		t.Fatalf("Run = %v, want the assembly's auth error", err)
	}
}

func TestRun_APortAlreadyInUseIsReturnedInsteadOfServing(t *testing.T) {
	dbtest.RequireIntegration(t)
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = taken.Close() })
	cfg := jwksConfig(t)
	cfg.DatabaseURL = dbtest.DatabaseURL(t)
	cfg.Host = "127.0.0.1"
	cfg.Port = taken.Addr().(*net.TCPAddr).Port

	var logs syncBuffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	err = runUntilItReturns(t, cfg)

	if err == nil || !strings.Contains(err.Error(), "address already in use") {
		t.Fatalf("Run = %v, want the listen error for the taken port", err)
	}
	if !strings.Contains(logs.String(), `msg="shutdown complete"`) {
		t.Errorf("Run returned the listen error without shutting the assembly down and closing the datastores; log:\n%s", logs.String())
	}
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
