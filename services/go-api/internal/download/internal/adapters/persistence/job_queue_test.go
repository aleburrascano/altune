package persistence_test

import (
	"altune/go-api/internal/download/internal/adapters/persistence"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/polltest"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresJobNotifier_RefusesAMissingSleeper(t *testing.T) {
	notifier, err := persistence.NewPostgresJobNotifier(nil, nil)

	if notifier != nil || !errors.Is(err, persistence.ErrNotifierSleeperRequired) || !strings.Contains(err.Error(), "NewPostgresJobNotifier") {
		t.Fatalf("got (%v, %v), want a nil notifier and an error naming the constructor wrapping ErrNotifierSleeperRequired", notifier, err)
	}
}

func TestPostgresJobNotifier_LogsWhenItCannotListen(t *testing.T) {
	logs := logging.CaptureText(t, slog.LevelInfo)
	pool, err := pgxpool.New(context.Background(), "postgres://altune:altune@127.0.0.1:1/altune?sslmode=disable")
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		persistence.MustNewPostgresJobNotifier(pool, clock.Sleep).Listen(ctx, make(chan struct{}, 1))
		close(done)
	}()

	polltest.Until(t, 10*time.Second, "the notifier to log that it cannot listen", func() bool {
		return strings.Contains(logs.String(), "acquisition.notifier_listen_failed")
	})
	cancel()
	<-done

	if !strings.Contains(logs.String(), "level=WARN") || !strings.Contains(logs.String(), "acquisition.notifier_listen_failed") {
		t.Errorf("logs = %q, want a warning that the notifier cannot listen", logs.String())
	}
}
