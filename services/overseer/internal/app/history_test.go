package app

import (
	"altune/overseer/internal/config"
	"altune/overseer/internal/core"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type seriesBucket struct {
	stubBucket
	received chan core.Series
}

func (s seriesBucket) UseSeries(series core.Series) { s.received <- series }

type panickingSeriesBucket struct{ stubBucket }

func (panickingSeriesBucket) UseSeries(core.Series) { panic("UseSeries blew up") }

type nopSeries struct{}

func (nopSeries) Record(string, string, core.Point) {}

func (nopSeries) Query(string, string, time.Time, time.Time) ([]core.Point, error) {
	return nil, nil
}

func historyConfig(path string) *config.Config {
	return &config.Config{
		Host:          "127.0.0.1",
		Port:          0,
		SupabaseURL:   "https://x.supabase.co",
		TickInterval:  time.Second,
		BucketTimeout: time.Second,
		HistoryPath:   path,
	}
}

func TestNewBootsOnACorruptHistoryFileAndSaysSo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")
	if err := os.WriteFile(path, []byte(strings.Repeat("garbage, not sqlite ", 300)), 0o600); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}
	logs := captureSlog(t)

	a := New(historyConfig(path))

	if a.server == nil {
		t.Fatal("New returned no server over a corrupt history file")
	}
	if !strings.Contains(logs.String(), "history: unavailable") {
		t.Fatalf("log does not say history is unavailable: %s", logs.String())
	}
	names, err := a.history.Names("reliability")
	if err != nil || len(names) != 0 {
		t.Fatalf("history over a corrupt file lists %v, %v; want empty", names, err)
	}
}

func TestWireSeriesHandsTheStoreToEverySeriesWriter(t *testing.T) {
	received := make(chan core.Series, 1)
	reg := core.NewRegistry()
	reg.Register(seriesBucket{stubBucket: stubBucket{id: "writer"}, received: received})
	reg.Register(stubBucket{id: "plain"})

	wireSeries(reg, nopSeries{})

	select {
	case got := <-received:
		if _, isStore := got.(nopSeries); !isStore {
			t.Fatalf("writer received %T, want the history store", got)
		}
	default:
		t.Fatal("series writer was never handed the store")
	}
}

func TestWireSeriesContainsAPanickingWriter(t *testing.T) {
	received := make(chan core.Series, 1)
	reg := core.NewRegistry()
	reg.Register(panickingSeriesBucket{stubBucket{id: "a-boom"}})
	reg.Register(seriesBucket{stubBucket: stubBucket{id: "b-writer"}, received: received})
	captureSlog(t)

	wireSeries(reg, nopSeries{})

	if len(received) != 1 {
		t.Fatal("a panicking UseSeries stopped the next bucket from receiving the store")
	}
}

func TestReleaseHistoryWaitsForThePrunerThenCloses(t *testing.T) {
	a := &App{history: nopHistory{}}
	ctx, cancel := context.WithCancel(context.Background())
	pruned := a.startPruner(ctx)

	finished := make(chan struct{})
	go func() {
		a.releaseHistory(cancel, pruned)
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("releaseHistory did not return: the pruner outlived shutdown")
	}
}

type nopHistory struct{ nopSeries }

func (nopHistory) Names(string) ([]string, error) { return nil, nil }

func (nopHistory) RunPruner(ctx context.Context) { <-ctx.Done() }

func (nopHistory) Close() error { return nil }
