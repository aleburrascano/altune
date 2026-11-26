package logging_test

import (
	"altune/go-api/internal/shared/logging"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func TestRing_CapturesDebugBelowStdoutLevel(t *testing.T) {
	logger, ring, stdout := setupLogger(t, "info", false)

	logger.Debug("provider.debug.line", "provider", "deezer")
	logger.Info("request.complete", "status", 200)

	snap := ring.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("ring should retain both DEBUG and INFO, got %d", len(snap))
	}

	out := stdout()
	if strings.Contains(out, "provider.debug.line") {
		t.Error("DEBUG must not reach stdout when stdout level is INFO")
	}
	if !strings.Contains(out, "request.complete") {
		t.Error("INFO must still reach stdout")
	}
}

const leakedAPIKey = "0123456789abcdeflastfmkey"

func lastfmURLError(t *testing.T) error {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		srv.URL+"/2.0/?method=artist.search&artist=x&api_key="+leakedAPIKey+"&format=json", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("expected the request to time out")
	}
	var ue *url.Error
	if !errors.As(err, &ue) || !strings.Contains(err.Error(), leakedAPIKey) {
		t.Fatalf("precondition: want *url.Error embedding the api_key, got %T %q", err, err)
	}
	return err
}

func assertNoKey(t *testing.T, recs []logging.CapturedRecord) {
	t.Helper()
	for _, rec := range recs {
		if strings.Contains(rec.Message, leakedAPIKey) {
			t.Errorf("api_key leaked into ring message %q", rec.Message)
		}
		for k, v := range rec.Attrs {
			if strings.Contains(v, leakedAPIKey) || strings.Contains(k, leakedAPIKey) {
				t.Errorf("api_key leaked into ring attr %q = %q", k, v)
			}
		}
	}
}

func TestRingHandler_RedactsSecretQueryParamInURLError(t *testing.T) {
	logger, ring := newCaptureLogger(t)
	ch, cancel, subErr := ring.Subscribe()
	if subErr != nil {
		t.Fatalf("Subscribe: %v", subErr)
	}
	defer cancel()

	err := lastfmURLError(t)
	logger.Warn("provider search failed", "provider", "lastfm", "error", err)
	logger.Error("fetch failed: " + err.Error())

	snap := ring.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("snapshot len = %d, want 2", len(snap))
	}
	assertNoKey(t, snap)
	if got := snap[0].Attrs["error"]; !strings.Contains(got, "api_key=REDACTED") || !strings.Contains(got, "method=artist.search") {
		t.Errorf("error attr not usefully redacted: %q", got)
	}
	if !strings.Contains(snap[1].Message, "api_key=REDACTED") {
		t.Errorf("message not redacted: %q", snap[1].Message)
	}

	for i := 0; i < 2; i++ {
		select {
		case rec := <-ch:
			assertNoKey(t, []logging.CapturedRecord{rec})
		default:
			t.Fatal("subscriber did not receive the record")
		}
	}
}

func TestRingHandler_RedactsSecretsInGroupsAndWithAttrs(t *testing.T) {
	logger, ring := newCaptureLogger(t)

	logger.With("lastfm_api_key", leakedAPIKey, "corr_id", "c1").
		Info("call",
			slog.Group("provider", "api_key", leakedAPIKey, "name", "lastfm"),
			"url", "https://ws.audioscrobbler.com/2.0/?api_key="+leakedAPIKey,
			"dsn", "postgres://u:"+leakedAPIKey+"@db:5432/x",
		)
	logger.Info("dial postgres://u:" + leakedAPIKey + "@db:5432/x failed")

	snap := ring.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("snapshot len = %d, want 2", len(snap))
	}
	assertNoKey(t, snap)
	if snap[0].Attrs["corr_id"] != "c1" || snap[0].Attrs["provider.name"] != "lastfm" {
		t.Errorf("non-secret attrs dropped: %+v", snap[0].Attrs)
	}
	if !strings.Contains(snap[1].Message, "db:5432") {
		t.Errorf("message over-redacted: %q", snap[1].Message)
	}
}

func newStreamCaptureLogger(t *testing.T) (*slog.Logger, *logging.RingBuffer, func() string) {
	t.Helper()
	return setupLogger(t, "debug", false)
}

func assertStreamHasNoKey(t *testing.T, stream string) {
	t.Helper()
	if strings.Contains(stream, leakedAPIKey) {
		t.Errorf("api_key reached the persisted log stream: %s", stream)
	}
}

func TestRingHandler_KeepsWithBoundSecretsOutOfTheStream(t *testing.T) {
	logger, _, stream := newStreamCaptureLogger(t)

	logger.With("lastfm_api_key", leakedAPIKey).
		With("corr_id", "c1", slog.Group("provider", "access_key", leakedAPIKey, "name", "lastfm")).
		Info("call")

	out := stream()
	assertStreamHasNoKey(t, out)
	if !strings.Contains(out, `"corr_id":"c1"`) || !strings.Contains(out, `"name":"lastfm"`) {
		t.Errorf("non-secret bound attrs dropped from the stream: %s", out)
	}
}

func TestRingHandler_KeepsGroupNestedSecretsOutOfTheStream(t *testing.T) {
	logger, _, stream := newStreamCaptureLogger(t)

	logger.Info("call",
		slog.Group("provider", "api_key", leakedAPIKey, "name", "lastfm"),
		slog.Group("failed", slog.Group("lastfm", "client_secret", leakedAPIKey, "attempt", 2)),
	)

	out := stream()
	assertStreamHasNoKey(t, out)
	if !strings.Contains(out, `"name":"lastfm"`) || !strings.Contains(out, `"attempt":2`) {
		t.Errorf("non-secret group members dropped from the stream: %s", out)
	}
}

func TestRingHandler_StillLogsWhenEveryBoundAttrIsRedacted(t *testing.T) {
	logger, ring, stream := newStreamCaptureLogger(t)

	logger.With("api_key", leakedAPIKey).WithGroup("provider").
		Info("call", "password", leakedAPIKey, "name", "lastfm")

	out := stream()
	assertStreamHasNoKey(t, out)
	if !strings.Contains(out, `"msg":"call"`) || !strings.Contains(out, `"name":"lastfm"`) {
		t.Errorf("record lost when every bound attr was redacted: %s", out)
	}
	snap := ring.Snapshot()
	if len(snap) != 1 || snap[0].Attrs["provider.name"] != "lastfm" {
		t.Errorf("ring lost the record when every bound attr was redacted: %+v", snap)
	}
}

func TestRingHandler_KeepsURLErrorSecretsOutOfTheStream(t *testing.T) {
	logger, _, stream := newStreamCaptureLogger(t)
	err := lastfmURLError(t)

	logger.With("startup_url", "https://ws.audioscrobbler.com/2.0/?api_key="+leakedAPIKey).
		Warn("provider search failed",
			"provider", "lastfm",
			"error", err,
			"detail", "fetch failed: "+err.Error(),
			slog.Group("upstream", "call_url", "https://ws.audioscrobbler.com/2.0/?api_key="+leakedAPIKey),
			"attempt", 2)
	logger.Error("fetch failed: " + err.Error())

	out := stream()
	assertStreamHasNoKey(t, out)
	if !strings.Contains(out, "api_key=REDACTED") || !strings.Contains(out, "method=artist.search") {
		t.Errorf("stream over- or under-redacted, want the call still diagnosable: %s", out)
	}
	if !strings.Contains(out, `"attempt":2`) || !strings.Contains(out, `"provider":"lastfm"`) {
		t.Errorf("non-secret attrs lost their value or kind: %s", out)
	}
}

type cleanThenLeakingValue struct{ resolves *int }

func (v cleanThenLeakingValue) LogValue() slog.Value {
	*v.resolves++
	if *v.resolves == 1 {
		return slog.StringValue("clean")
	}
	return slog.StringValue(leakedAPIKey)
}

func TestRingHandler_ResolvesEachAttrOnce(t *testing.T) {
	logger, _, stream := newStreamCaptureLogger(t)

	logger.Info("call", "thing", cleanThenLeakingValue{resolves: new(int)})

	assertStreamHasNoKey(t, stream())
}

func TestRingBuffer_EvictsRecordsPastRetention(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const retention = 30 * time.Minute
		logger, ring := newCaptureLogger(t)

		logger.Info("old")
		time.Sleep(retention / 2)
		logger.Info("mid")
		time.Sleep(retention/2 + time.Nanosecond)

		snap := ring.Snapshot()
		if len(snap) != 1 || snap[0].Message != "mid" {
			t.Fatalf("snapshot = %+v, want only mid (old aged out one nanosecond past the window)", snap)
		}

		time.Sleep(retention)
		if snap := ring.Snapshot(); len(snap) != 0 {
			t.Fatalf("snapshot = %+v, want empty after every record aged out", snap)
		}

		logMessages(logger, ringCapacity+1)
		snap = ring.Snapshot()
		if len(snap) != ringCapacity || snap[0].Message != "m1" || snap[ringCapacity-1].Message != "m1000" {
			t.Fatalf("snapshot after refill holds %d records from %q to %q, want %d records m1..m1000",
				len(snap), snap[0].Message, snap[len(snap)-1].Message, ringCapacity)
		}
	})
}

func TestSetup_RingBufferEvictsByTheInjectedClockNotTheWallClock(t *testing.T) {
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	ring := logging.Setup("info", logging.WithClock(func() time.Time { return now }))

	slog.Info("old")
	now = now.Add(30*time.Minute + time.Nanosecond)
	slog.Info("fresh")

	snap := ring.Snapshot()
	if len(snap) != 1 || snap[0].Message != "fresh" {
		t.Fatalf("snapshot = %+v, want only fresh: old is a nanosecond past the window on the injected clock", snap)
	}
}

func TestRingBufferClockIsRequired(t *testing.T) {
	tests := map[string]func(){
		"NewRingBuffer": func() { logging.NewRingBuffer(4, nil) },
		"WithClock":     func() { logging.WithClock(nil) },
	}
	for name, build := range tests {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("a nil clock was accepted, want a panic at construction")
				}
			}()

			build()
		})
	}
}

func TestRingBuffer_KeepsARecordExactlyAtTheRetentionEdge(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		logger, ring := newCaptureLogger(t)

		logger.Info("edge")
		time.Sleep(30 * time.Minute)

		if snap := ring.Snapshot(); len(snap) != 1 {
			t.Fatalf("snapshot = %+v, want the record kept until it is older than 30m", snap)
		}
	})
}
