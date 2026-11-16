package persistence

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestPgxJobNotifier_LogsWhenItCannotListen(t *testing.T) {
	logs := &lockedBuffer{}
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	pool, err := pgxpool.New(context.Background(), "postgres://altune:altune@127.0.0.1:1/altune?sslmode=disable")
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		NewPgxJobNotifier(pool).Listen(ctx, make(chan struct{}, 1))
		close(done)
	}()

	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(logs.String(), "acquisition.notifier_listen_failed") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done

	if !strings.Contains(logs.String(), "level=WARN") || !strings.Contains(logs.String(), "acquisition.notifier_listen_failed") {
		t.Errorf("logs = %q, want a warning that the notifier cannot listen", logs.String())
	}
}
