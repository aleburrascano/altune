package app_test

import (
	"altune/overseer/internal/core"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

type seriesBucket struct {
	*fakeBucket
	panics bool
}

func (s seriesBucket) UseSeries(series core.Series) {
	if s.panics {
		panic("UseSeries blew up")
	}
	series.Record(s.id, "up", core.Point{At: time.Now(), Value: 1})
}

func wantOnePoint(t *testing.T, r *running, bucket string) {
	t.Helper()
	if got := r.series(t, bucket)["up"]; len(got) != 1 || got[0].V != 1 {
		t.Fatalf("history series %s/up = %+v; want the one point the bucket recorded through the series it was handed", bucket, got)
	}
}

func TestACorruptHistoryFileStillBootsAndSaysSo(t *testing.T) {
	cfg := newConfig(t)
	if err := os.WriteFile(cfg.HistoryPath, []byte(strings.Repeat("garbage, not sqlite ", 300)), 0o600); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}
	logs := captureLog(t)

	r := boot(t, cfg, plain("reliability"))

	if code, _ := r.health(t); code != http.StatusOK {
		t.Errorf("GET /health over a corrupt history file = %d; want 200", code)
	}
	if !strings.Contains(logs.String(), "history: unavailable") {
		t.Errorf("log does not say history is unavailable: %s", logs.String())
	}
	if got := r.series(t, "reliability"); len(got) != 0 {
		t.Errorf("history over a corrupt file lists %v; want empty", got)
	}
}

func TestEverySeriesWriterIsHandedTheHistoryStore(t *testing.T) {
	captureLog(t)

	r := boot(t, newConfig(t), seriesBucket{fakeBucket: plain("writer")}, plain("plain"))

	wantOnePoint(t, r, "writer")
}

func TestAPanickingSeriesWriterDoesNotStopTheNextOneGettingTheStore(t *testing.T) {
	logs := captureLog(t)

	r := boot(t, newConfig(t), seriesBucket{fakeBucket: plain("a-boom"), panics: true}, seriesBucket{fakeBucket: plain("b-writer")})

	wantOnePoint(t, r, "b-writer")
	panics := logs.named("overseer.start.bucket_panic")
	if len(panics) != 1 || panics[0]["stage"] != "UseSeries" || panics[0]["bucket"] != "a-boom" {
		t.Errorf("start bucket_panic lines = %v; want one for a-boom at stage UseSeries", panics)
	}
}

func TestRunReturnsNilSoonAfterItsContextIsCancelled(t *testing.T) {
	captureLog(t)
	r := boot(t, newConfig(t), plain("plain"))

	if err := r.stop(2 * time.Second); err != nil {
		t.Fatalf("Run after cancel: %v; the history pruner or store outlived shutdown", err)
	}
}
