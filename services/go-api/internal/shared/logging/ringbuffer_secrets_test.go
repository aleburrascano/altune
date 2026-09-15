package logging

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

const leakedAPIKey = "0123456789abcdeflastfmkey"

// lastfmURLError produces the real *url.Error a timed-out Last.fm call yields:
// Go embeds the full request URL, api_key included, in err.Error().
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

func assertNoKey(t *testing.T, recs []CapturedRecord) {
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

// TestRingHandler_RedactsSecretQueryParamInURLError reproduces #997: a wrapped
// *url.Error logged under the conventional "error" key carried the live
// Last.fm api_key straight into the admin logs feed.
func TestRingHandler_RedactsSecretQueryParamInURLError(t *testing.T) {
	logger, ring := newCaptureLogger(t, 10)
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

	// The live stream is fed from the same choke point.
	for i := 0; i < 2; i++ {
		select {
		case rec := <-ch:
			assertNoKey(t, []CapturedRecord{rec})
		default:
			t.Fatal("subscriber did not receive the record")
		}
	}
}

// TestRingHandler_RedactsSecretsInGroupsAndWithAttrs covers attrs a top-level
// record scan misses: nested group leaves and logger.With attrs.
func TestRingHandler_RedactsSecretsInGroupsAndWithAttrs(t *testing.T) {
	logger, ring := newCaptureLogger(t, 10)

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

func TestRingBuffer_EvictsRecordsPastRetention(t *testing.T) {
	clock := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	ring := newRingBufferWithClock(10, func() time.Time { return clock })

	ring.append(CapturedRecord{Message: "old"})
	clock = clock.Add(logRetentionWindow / 2)
	ring.append(CapturedRecord{Message: "mid"})
	clock = clock.Add(logRetentionWindow/2 + time.Minute)

	snap := ring.Snapshot()
	if len(snap) != 1 || snap[0].Message != "mid" {
		t.Fatalf("snapshot = %+v, want only mid (old aged out)", snap)
	}

	clock = clock.Add(logRetentionWindow)
	if snap := ring.Snapshot(); len(snap) != 0 {
		t.Fatalf("snapshot = %+v, want empty after every record aged out", snap)
	}

	// Capacity eviction still applies alongside age eviction, and the ring
	// keeps working after being fully drained by age.
	for _, m := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k"} {
		ring.append(CapturedRecord{Message: m})
	}
	snap = ring.Snapshot()
	if len(snap) != 10 || snap[0].Message != "b" || snap[9].Message != "k" {
		t.Fatalf("snapshot after refill = %+v, want 10 records b..k", snap)
	}
}
