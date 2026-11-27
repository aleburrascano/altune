package app_test

import (
	"altune/go-api/internal/app"
	"altune/go-api/internal/library"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

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

func blackHolePool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), blackHoleDatabase(t))
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestCatalog_AnUnconfiguredAudioStoreFailsAssembly(t *testing.T) {
	cfg := jwksConfig(t)
	cfg.MusicDir, cfg.YtMusicEnabled = "", true

	_, err := app.Assemble(t.Context(), cfg, app.Handles{Pool: unreachablePool(t), Transport: refuseEgress{}, Tracker: testTracker(t)})

	if err == nil || !strings.Contains(err.Error(), "MUSIC_DIR") {
		t.Fatalf("Assemble with no audio store configured = %v, want an error naming MUSIC_DIR", err)
	}
}

func TestCatalog_APartialOCIS3ConfigFailsAssemblyEvenWithMusicDir(t *testing.T) {
	cfg := jwksConfig(t)
	cfg.YtMusicEnabled = true
	cfg.OCIS3Endpoint, cfg.OCIS3Bucket = "https://objectstorage.example.com", "audio"

	_, err := app.Assemble(t.Context(), cfg, app.Handles{Pool: unreachablePool(t), Transport: refuseEgress{}, Tracker: testTracker(t)})

	if err == nil {
		t.Fatal("Assemble with 2 of 4 OCI keys and MUSIC_DIR: want error, got nil")
	}
	for _, want := range []string{"OCI_S3_ACCESS_KEY", "OCI_S3_SECRET_KEY"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err.Error(), want)
		}
	}
}

func TestCatalog_ADBCallTimeoutReachesOperatorLiveMetrics(t *testing.T) {
	if testing.Short() {
		t.Skip("waits out the 5s production DB-call deadline")
	}
	fixture := newJWKSFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	assembly, err := app.Assemble(ctx, fixture.cfg, app.Handles{Pool: blackHolePool(t), Transport: refuseEgress{}, Tracker: testTracker(t)})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		assembly.Shutdown(context.Background())
	})
	h, token := assembly.Handler(), signedToken(t, fixture.key, fixture.operator)

	before := library.MetricsSnapshot{}.DBCallTimeouts
	start := time.Now()
	code, body := getAs(t, h, "/v1/library/albums", token)
	if code < http.StatusInternalServerError {
		t.Fatalf("library read against a wedged DB: status %d, want 5xx; body %s", code, body)
	}
	if elapsed := time.Since(start); elapsed > 15*time.Second {
		t.Fatalf("library read took %v, the DB-call deadline did not bound it", elapsed)
	}

	code, body = getAs(t, h, "/observe/metrics/live", token)
	if code != http.StatusOK {
		t.Fatalf("operator metrics read: status %d, want 200; body %s", code, body)
	}
	var got struct {
		Catalog library.MetricsSnapshot `json:"catalog"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	if got.Catalog.DBCallTimeouts != before+1 {
		t.Errorf("catalog.db_call_timeouts_total %d, want %d; body %s", got.Catalog.DBCallTimeouts, before+1, body)
	}
}
