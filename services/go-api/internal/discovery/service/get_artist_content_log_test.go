package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/url"
	"strings"
	"testing"
)

const fanOutFailedEvent = "artist_content.fanout.provider_failed"

// captureProductionLogs routes slog to a JSON buffer at Info, the default
// production level, so anything logged below it is dropped as in production.
func captureProductionLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// fanOutFailureRecords returns every logged fan-out failure record.
func fanOutFailureRecords(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("unparseable log line %q: %v", line, err)
		}
		if rec["msg"] == fanOutFailedEvent {
			out = append(out, rec)
		}
	}
	return out
}

// Issue #1100: a provider failing inside the identity fan-out was logged at
// Debug, so production (Info) never saw which provider dropped out for which
// artist while the merge quietly served the rest.
func TestIdentityFanOut_ProviderFailuresWarnOnceAtProductionLevel(t *testing.T) {
	const secret = "0123456789abcdef0123456789abcdef"
	leaky := &url.Error{Op: "Get", URL: "https://ws.audioscrobbler.com/2.0/?api_key=" + secret, Err: errors.New("connection refused")}
	for name, fetch := range identityContentFetchers {
		t.Run(name, func(t *testing.T) {
			svc := identityFanOutWithErr(func(pn domain.ProviderName) error {
				if pn == domain.ProviderLastFM || pn == domain.ProviderSpotify {
					return leaky
				}
				return nil
			})
			buf := captureProductionLogs(t)

			if _, err := fetch(svc); err != nil {
				t.Fatalf("error = %v", err)
			}

			recs := fanOutFailureRecords(t, buf)
			if len(recs) != 1 {
				t.Fatalf("got %d %s records at Info level, want exactly 1 summary:\n%s", len(recs), fanOutFailedEvent, buf)
			}
			rec := recs[0]
			if rec["level"] != "WARN" {
				t.Errorf("level = %v, want WARN", rec["level"])
			}
			failed, _ := rec["failed"].(map[string]any)
			if len(failed) != 2 {
				t.Fatalf("failed = %v, want exactly lastfm and spotify", rec["failed"])
			}
			for _, pn := range []domain.ProviderName{domain.ProviderLastFM, domain.ProviderSpotify} {
				entry, _ := failed[pn.String()].(map[string]any)
				if entry["external_id"] != "id-"+pn.String() {
					t.Errorf("%s external_id = %v, want %q", pn, entry["external_id"], "id-"+pn.String())
				}
				if errText, _ := entry["error"].(string); !strings.Contains(errText, "connection refused") {
					t.Errorf("%s error = %q, want the provider's failure", pn, errText)
				}
			}
			if strings.Contains(buf.String(), secret) {
				t.Errorf("provider credential leaked into logs:\n%s", buf)
			}
		})
	}
}

func TestIdentityFanOut_NoFailureWarnWhenAllAnswer(t *testing.T) {
	svc := identityFanOutWithErr(func(domain.ProviderName) error { return nil })
	buf := captureProductionLogs(t)

	if _, err := svc.GetTopTracks(context.Background(), domain.ProviderDeezer, "id-deezer", "Che", 10); err != nil {
		t.Fatalf("error = %v", err)
	}
	if recs := fanOutFailureRecords(t, buf); len(recs) != 0 {
		t.Errorf("got %d failure records, want none:\n%s", len(recs), buf)
	}
}

// A client that hangs up cancels every in-flight provider call; that says
// nothing about provider health and would otherwise warn once per abandoned
// request.
func TestIdentityFanOut_NoFailureWarnWhenCallerCancels(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc := identityFanOutWithErr(func(pn domain.ProviderName) error {
		if pn == domain.ProviderDeezer {
			return nil
		}
		cancel()
		return context.Canceled
	})
	buf := captureProductionLogs(t)

	if _, err := svc.GetTopTracks(ctx, domain.ProviderDeezer, "id-deezer", "Che", 10); err != nil {
		t.Fatalf("error = %v", err)
	}
	if recs := fanOutFailureRecords(t, buf); len(recs) != 0 {
		t.Errorf("got %d failure records for a cancelled request, want none:\n%s", len(recs), buf)
	}
}

// An open circuit skips the provider without calling it; the breaker already
// reported the trip, so repeating it on every request would be spam.
func TestIdentityFanOut_NoFailureWarnForCircuitOpenSkip(t *testing.T) {
	cb := NewCircuitBreaker()
	tripViaSearch(t, cb, domain.ProviderSpotify)
	svc := identityFanOutWithErr(func(domain.ProviderName) error { return nil }, WithContentCircuitBreaker(cb))
	buf := captureProductionLogs(t)

	if _, err := svc.GetTopTracks(context.Background(), domain.ProviderDeezer, "id-deezer", "Che", 10); err != nil {
		t.Fatalf("error = %v", err)
	}
	if recs := fanOutFailureRecords(t, buf); len(recs) != 0 {
		t.Errorf("got %d failure records for a circuit-open skip, want none:\n%s", len(recs), buf)
	}
}

// identityFanOutWithErr is identityFanOut with the failure error chosen per
// provider; nil means the provider answers.
func identityFanOutWithErr(errFor func(domain.ProviderName) error, opts ...ArtistContentOption) *GetArtistContentService {
	providers := make(map[domain.ProviderName]ports.ArtistContentProvider, len(everyContentProvider))
	xref := make(map[string]string, len(everyContentProvider))
	for _, name := range everyContentProvider {
		xref[name.String()] = "id-" + name.String()
		providers[name] = &fakeArtistContentProvider{
			getTopTracksFn: func(_ context.Context, pn domain.ProviderName, id string) ([]domain.SearchResult, error) {
				if err := errFor(pn); err != nil {
					return nil, err
				}
				return []domain.SearchResult{trackFrom(pn, id, "Real Song", "Che")}, nil
			},
			getAlbumsFn: func(_ context.Context, pn domain.ProviderName, id string) ([]domain.SearchResult, error) {
				if err := errFor(pn); err != nil {
					return nil, err
				}
				return []domain.SearchResult{v2Album(pn, id, "Fully Loaded", withDate("2026-04-01"))}, nil
			},
		}
	}
	store := &fakeIdentityStore{mbid: "mbid-che", xref: xref}
	return NewGetArtistContentService(providers, append(opts, WithContentIdentityStore(store))...)
}
