package logging

import (
	"bytes"
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

// newStreamCaptureLogger builds the production handler shape — a ringHandler
// wrapping the JSON handler that writes the persisted stream — with that
// stream captured, so a test can assert on what leaves the process rather than
// only on the ring the admin viewer reads.
func newStreamCaptureLogger(t *testing.T, ring *RingBuffer) (*slog.Logger, func() string) {
	t.Helper()
	var stream bytes.Buffer
	inner := slog.NewJSONHandler(&stream, &slog.HandlerOptions{Level: slog.LevelDebug})
	return slog.New(newRingHandler(inner, ring)), stream.String
}

func assertStreamHasNoKey(t *testing.T, stream string) {
	t.Helper()
	if strings.Contains(stream, leakedAPIKey) {
		t.Errorf("api_key reached the persisted log stream: %s", stream)
	}
}

// TestRingHandler_KeepsWithBoundSecretsOutOfTheStream pins #1612: an attr bound
// once via logger.With was handed to the stdout handler unfiltered, so it rode
// every later record from that logger while the ring's own view looked clean.
func TestRingHandler_KeepsWithBoundSecretsOutOfTheStream(t *testing.T) {
	logger, stream := newStreamCaptureLogger(t, NewRingBuffer(10))

	logger.With("lastfm_api_key", leakedAPIKey).
		With("corr_id", "c1", slog.Group("provider", "access_key", leakedAPIKey, "name", "lastfm")).
		Info("call")

	out := stream()
	assertStreamHasNoKey(t, out)
	if !strings.Contains(out, `"corr_id":"c1"`) || !strings.Contains(out, `"name":"lastfm"`) {
		t.Errorf("non-secret bound attrs dropped from the stream: %s", out)
	}
}

// TestRingHandler_KeepsGroupNestedSecretsOutOfTheStream pins the second half of
// #1612: the record sanitizer only scanned top-level attrs, so a secret inside
// a group whose own key is no marker (the shape fanOutFailureAttr builds)
// reached stdout while the ring's flattened copy redacted it.
func TestRingHandler_KeepsGroupNestedSecretsOutOfTheStream(t *testing.T) {
	logger, stream := newStreamCaptureLogger(t, NewRingBuffer(10))

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

// TestRingHandler_StillLogsWhenEveryBoundAttrIsRedacted covers the degenerate
// end of the scrub: a derived logger whose entire bound set is secret, under an
// open group, must still deliver its records to both sinks.
func TestRingHandler_StillLogsWhenEveryBoundAttrIsRedacted(t *testing.T) {
	ring := NewRingBuffer(10)
	logger, stream := newStreamCaptureLogger(t, ring)

	logger.With("api_key", leakedAPIKey).WithGroup("provider").
		Info("call", "password", leakedAPIKey, "name", "lastfm")

	out := stream()
	assertStreamHasNoKey(t, out)
	if !strings.Contains(out, `"msg":"call"`) || !strings.Contains(out, `"name":"lastfm"`) {
		t.Errorf("record lost when every bound attr was redacted: %s", out)
	}
	snap := ring.Snapshot()
	if len(snap) != 1 || snap[0].Attrs["name"] != "lastfm" {
		t.Errorf("ring lost the record when every bound attr was redacted: %+v", snap)
	}
}

// TestRingHandler_KeepsURLErrorSecretsOutOfTheStream pins #2227: a *url.Error
// logged under the conventional "error" key names nothing secret, so the
// key-based drop let it through and the api_key inside its URL reached the
// stdout stream docker persists, while the ring's own copy looked clean.
func TestRingHandler_KeepsURLErrorSecretsOutOfTheStream(t *testing.T) {
	logger, stream := newStreamCaptureLogger(t, NewRingBuffer(10))
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

// cleanThenLeakingValue answers the first resolve cleanly and every later one
// with the secret — the shape that beats a redaction check which resolves for
// the check and hands the unresolved attr to the handler to resolve again.
type cleanThenLeakingValue struct{ resolves *int }

func (v cleanThenLeakingValue) LogValue() slog.Value {
	*v.resolves++
	if *v.resolves == 1 {
		return slog.StringValue("clean")
	}
	return slog.StringValue(leakedAPIKey)
}

func TestRingHandler_ResolvesEachAttrOnce(t *testing.T) {
	logger, stream := newStreamCaptureLogger(t, NewRingBuffer(10))

	logger.Info("call", "thing", cleanThenLeakingValue{resolves: new(int)})

	assertStreamHasNoKey(t, stream())
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
