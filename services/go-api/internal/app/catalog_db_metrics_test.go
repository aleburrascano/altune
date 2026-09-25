package app

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/config"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"

	adminHandler "altune/go-api/internal/admin/handler"
	catalogMetrics "altune/go-api/internal/catalog/adapters/metrics"
	discoveryHandler "altune/go-api/internal/discovery/adapters/handler"
	playbackHandler "altune/go-api/internal/playback/adapters/handler"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// blackHoleDatabase accepts TCP connections and never answers, standing in for
// a wedged Postgres: the pgx handshake blocks until the caller's deadline.
func blackHoleDatabase(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	var held []net.Conn
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			held = append(held, c)
		}
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		<-done
		for _, c := range held {
			_ = c.Close()
		}
	})
	return "postgres://altune:altune@" + ln.Addr().String() + "/altune?sslmode=disable"
}

// A catalog request against a wedged database is cut off by the persistence
// per-call deadline, and the operator reads that timeout back from the catalog
// section of GET /admin/metrics/live on the production router.
func TestCatalogDBTimeout_ReachesOperatorLiveMetrics(t *testing.T) {
	if testing.Short() {
		t.Skip("waits out the 5s production DB-call deadline")
	}
	pool, err := pgxpool.New(context.Background(), blackHoleDatabase(t))
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	a := &App{
		cfg:  &config.Config{MusicDir: t.TempDir()},
		sem:  make(chan struct{}, 1),
		pool: pool,
	}
	cat, err := a.wireCatalog(nil, nil, nil)
	if err != nil {
		t.Fatalf("wireCatalog: %v", err)
	}
	operator := shared.NewUserId(uuid.New())
	verifier := auth.VerifierFunc(func(_ context.Context, token string) (auth.VerifiedToken, error) {
		if token == operatorToken {
			return auth.VerifiedToken{UserID: operator, ExpiresAt: time.Now().Add(time.Hour)}, nil
		}
		return auth.VerifiedToken{}, errors.New("bad token")
	})
	r := a.mountRoutes(verifier, cat,
		playbackHandler.NewQueueHandler(nil),
		discoveryHandler.NewDiscoveryHandler(discoveryHandler.DiscoveryServices{}), nil)
	mountAdmin(r, verifier, adminPrincipals{operator: operator.String()}, adminHandler.New(nil, nil).WithLiveMetrics(liveMetricsSnapshot))

	before := catalogMetrics.ReadSnapshot().DBCallTimeouts
	start := time.Now()
	code, body := callAdmin(t, r, http.MethodGet, "/v1/library/albums", operatorToken)
	if code < http.StatusInternalServerError {
		t.Fatalf("library read against a wedged DB: status %d, want 5xx; body %s", code, body)
	}
	if elapsed := time.Since(start); elapsed > 15*time.Second {
		t.Fatalf("library read took %v, the DB-call deadline did not bound it", elapsed)
	}

	code, body = callAdmin(t, r, http.MethodGet, "/admin/metrics/live", operatorToken)
	if code != http.StatusOK {
		t.Fatalf("operator metrics read: status %d, want 200; body %s", code, body)
	}
	var got struct {
		Catalog catalogMetrics.Snapshot `json:"catalog"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	if got.Catalog.DBCallTimeouts != before+1 {
		t.Errorf("catalog.db_call_timeouts_total %d, want %d; body %s", got.Catalog.DBCallTimeouts, before+1, body)
	}
}
