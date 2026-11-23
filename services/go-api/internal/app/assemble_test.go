package app_test

import (
	"altune/go-api/internal/app"
	"altune/go-api/internal/app/shutdown"
	"altune/go-api/internal/shared/sharedtest"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
)

type refuseEgress struct{}

func (refuseEgress) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("test transport: provider egress refused")
}

func integrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	sharedtest.RequireIntegration(t)
	pool, err := pgxpool.New(context.Background(), os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func unreachablePool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), "postgres://nobody@127.0.0.1:1/none?connect_timeout=1")
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestAssemble_RefusesMissingHandles(t *testing.T) {
	cases := map[string]struct {
		handles app.Handles
		names   string
	}{
		"no pool":      {app.Handles{Transport: refuseEgress{}}, "Pool"},
		"no transport": {app.Handles{Pool: unreachablePool(t)}, "Transport"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := app.Assemble(t.Context(), jwksConfig(t), tc.handles)

			if err == nil || !strings.Contains(err.Error(), "Handles."+tc.names) {
				t.Fatalf("Assemble = %v, want an error naming Handles.%s", err, tc.names)
			}
		})
	}
}

func TestAssemble_ServesHealthStartsTheJobsAndShutsDownCleanly(t *testing.T) {
	pool := integrationPool(t)
	redisClient := goredis.NewClient(&goredis.Options{Addr: miniredis.RunT(t).Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	assembly, err := app.Assemble(ctx, jwksConfig(t), app.Handles{Pool: pool, Redis: redisClient, Transport: refuseEgress{}})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}

	rec := httptest.NewRecorder()
	assembly.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"status":"ok"`) {
		t.Errorf("GET /health = %d %s, want 200 ok with a reachable database", rec.Code, rec.Body)
	}
	if jobs := assembly.Jobs().Health(); len(jobs) == 0 {
		t.Error("the assembly registered no background jobs")
	}

	cancel()
	outcomes := assembly.Shutdown()

	if len(outcomes) == 0 {
		t.Fatal("Shutdown reported no components")
	}
	for _, o := range outcomes {
		if o.Status != shutdown.Completed {
			t.Errorf("component %q ended %v, want completed after a clean shutdown", o.Component, o.Status)
		}
	}
}

func TestAssemble_AnUnreachableDatabaseDegradesHealth(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	assembly, err := app.Assemble(ctx, jwksConfig(t), app.Handles{Pool: unreachablePool(t), Transport: refuseEgress{}})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}

	rec := httptest.NewRecorder()
	assembly.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), `"status":"degraded"`) {
		t.Errorf("GET /health with an unreachable database = %d %s, want 503 degraded", rec.Code, rec.Body)
	}
	cancel()
	assembly.Shutdown()
}
