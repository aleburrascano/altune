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

func TestGetArtistContentService_GetTopTracks(t *testing.T) {
	sampleTracks := []domain.SearchResult{
		{Kind: domain.ResultKindTrack, Title: "Blinding Lights", Subtitle: "The Weeknd", Sources: []domain.SourceRef{{Provider: domain.ProviderDeezer, ExternalID: "t1"}}},
		{Kind: domain.ResultKindTrack, Title: "Save Your Tears", Subtitle: "The Weeknd", Sources: []domain.SourceRef{{Provider: domain.ProviderDeezer, ExternalID: "t2"}}},
		{Kind: domain.ResultKindTrack, Title: "Starboy", Subtitle: "The Weeknd", Sources: []domain.SourceRef{{Provider: domain.ProviderDeezer, ExternalID: "t3"}}},
	}

	tests := []struct {
		name          string
		providerName  domain.ProviderName
		externalID    string
		limit         int
		providers     map[domain.ProviderName]ports.ArtistContentProvider
		wantStatus    domain.ProviderStatus
		wantItemCount int
	}{
		{
			name:         "valid provider returns top tracks",
			providerName: domain.ProviderDeezer,
			externalID:   "artist-42",
			limit:        0,
			providers: map[domain.ProviderName]ports.ArtistContentProvider{
				domain.ProviderDeezer: &fakeArtistContentProvider{
					getTopTracksFn: func(_ context.Context, pn domain.ProviderName, extID string) ([]domain.SearchResult, error) {
						if pn != domain.ProviderDeezer {
							t.Errorf("expected provider deezer, got %s", pn.String())
						}
						if extID != "artist-42" {
							t.Errorf("expected externalID artist-42, got %s", extID)
						}
						return sampleTracks, nil
					},
				},
			},
			wantStatus:    domain.ProviderStatusOK,
			wantItemCount: 3,
		},
		{
			name:          "unknown provider returns error status",
			providerName:  domain.ProviderSoundCloud,
			externalID:    "artist-42",
			limit:         0,
			providers:     map[domain.ProviderName]ports.ArtistContentProvider{},
			wantStatus:    domain.ProviderStatusError,
			wantItemCount: 0,
		},
		{
			name:         "provider error returns error status",
			providerName: domain.ProviderDeezer,
			externalID:   "artist-err",
			limit:        0,
			providers: map[domain.ProviderName]ports.ArtistContentProvider{
				domain.ProviderDeezer: &fakeArtistContentProvider{
					getTopTracksFn: func(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
						return nil, errors.New("network error")
					},
				},
			},
			wantStatus:    domain.ProviderStatusError,
			wantItemCount: 0,
		},
		{
			name:         "limit truncates results",
			providerName: domain.ProviderDeezer,
			externalID:   "artist-42",
			limit:        2,
			providers: map[domain.ProviderName]ports.ArtistContentProvider{
				domain.ProviderDeezer: &fakeArtistContentProvider{
					getTopTracksFn: func(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
						return sampleTracks, nil
					},
				},
			},
			wantStatus:    domain.ProviderStatusOK,
			wantItemCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewGetArtistContentService(tt.providers)

			resp, err := svc.GetTopTracks(context.Background(), tt.providerName, tt.externalID, "", tt.limit)

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resp.Status != tt.wantStatus {
				t.Errorf("expected status %s, got %s", tt.wantStatus.String(), resp.Status.String())
			}
			if len(resp.Items) != tt.wantItemCount {
				t.Errorf("expected %d items, got %d", tt.wantItemCount, len(resp.Items))
			}
			if resp.ProviderName != tt.providerName {
				t.Errorf("expected provider name %q, got %q", tt.providerName, resp.ProviderName)
			}
		})
	}
}

func TestGetArtistContentService_GetAlbums(t *testing.T) {
	tests := []struct {
		name          string
		providerName  domain.ProviderName
		externalID    string
		limit         int
		providers     map[domain.ProviderName]ports.ArtistContentProvider
		wantStatus    domain.ProviderStatus
		wantItemCount int
	}{
		{
			name:         "valid provider returns albums",
			providerName: domain.ProviderDeezer,
			externalID:   "artist-42",
			limit:        0,
			providers: map[domain.ProviderName]ports.ArtistContentProvider{
				domain.ProviderDeezer: &fakeArtistContentProvider{
					getAlbumsFn: func(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
						return []domain.SearchResult{
							{Kind: domain.ResultKindAlbum, Title: "After Hours", Sources: []domain.SourceRef{{Provider: domain.ProviderDeezer, ExternalID: "a1"}}},
							{Kind: domain.ResultKindAlbum, Title: "Starboy", Sources: []domain.SourceRef{{Provider: domain.ProviderDeezer, ExternalID: "a2"}}},
						}, nil
					},
				},
			},
			wantStatus:    domain.ProviderStatusOK,
			wantItemCount: 2,
		},
		{
			name:          "unknown provider returns error status",
			providerName:  domain.ProviderSoundCloud,
			externalID:    "artist-42",
			limit:         0,
			providers:     map[domain.ProviderName]ports.ArtistContentProvider{},
			wantStatus:    domain.ProviderStatusError,
			wantItemCount: 0,
		},
		{
			name:         "dedup by normalized title keeps album with higher track_count",
			providerName: domain.ProviderDeezer,
			externalID:   "artist-42",
			limit:        0,
			providers: map[domain.ProviderName]ports.ArtistContentProvider{
				domain.ProviderDeezer: &fakeArtistContentProvider{
					getAlbumsFn: func(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
						return []domain.SearchResult{
							{
								Kind:       domain.ResultKindAlbum,
								Title:      "After Hours",
								Sources:    []domain.SourceRef{{Provider: domain.ProviderDeezer, ExternalID: "a1"}},
								TrackCount: 14,
							},
							{
								Kind:       domain.ResultKindAlbum,
								Title:      "After Hours (Deluxe)",
								Sources:    []domain.SourceRef{{Provider: domain.ProviderDeezer, ExternalID: "a2"}},
								TrackCount: 18,
							},
							{
								Kind:       domain.ResultKindAlbum,
								Title:      "Starboy",
								Sources:    []domain.SourceRef{{Provider: domain.ProviderDeezer, ExternalID: "a3"}},
								TrackCount: 18,
							},
						}, nil
					},
				},
			},
			wantStatus:    domain.ProviderStatusOK,
			wantItemCount: 2,
		},
		{
			name:         "dedup keeps higher track_count version",
			providerName: domain.ProviderDeezer,
			externalID:   "artist-42",
			limit:        0,
			providers: map[domain.ProviderName]ports.ArtistContentProvider{
				domain.ProviderDeezer: &fakeArtistContentProvider{
					getAlbumsFn: func(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
						return []domain.SearchResult{
							{
								Kind:       domain.ResultKindAlbum,
								Title:      "Dawn FM",
								Sources:    []domain.SourceRef{{Provider: domain.ProviderDeezer, ExternalID: "a1"}},
								TrackCount: 10,
							},
							{
								Kind:       domain.ResultKindAlbum,
								Title:      "Dawn FM (Alternate World)",
								Sources:    []domain.SourceRef{{Provider: domain.ProviderDeezer, ExternalID: "a2"}},
								TrackCount: 20,
							},
						}, nil
					},
				},
			},
			wantStatus:    domain.ProviderStatusOK,
			wantItemCount: 1,
		},
		{
			name:         "provider error returns error status",
			providerName: domain.ProviderDeezer,
			externalID:   "artist-err",
			limit:        0,
			providers: map[domain.ProviderName]ports.ArtistContentProvider{
				domain.ProviderDeezer: &fakeArtistContentProvider{
					getAlbumsFn: func(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
						return nil, errors.New("api failure")
					},
				},
			},
			wantStatus:    domain.ProviderStatusError,
			wantItemCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewGetArtistContentService(tt.providers)

			resp, err := svc.GetAlbums(context.Background(), tt.providerName, tt.externalID, "", tt.limit)

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resp.Status != tt.wantStatus {
				t.Errorf("expected status %s, got %s", tt.wantStatus.String(), resp.Status.String())
			}
			if len(resp.Items) != tt.wantItemCount {
				t.Errorf("expected %d items, got %d", tt.wantItemCount, len(resp.Items))
			}
		})
	}
}

func TestGetArtistContentService_GetAlbums_OrderingAndYear(t *testing.T) {
	album := func(title, releaseDate, extID string) domain.SearchResult {
		return domain.SearchResult{
			Kind:        domain.ResultKindAlbum,
			Title:       title,
			Sources:     []domain.SourceRef{{Provider: domain.ProviderDeezer, ExternalID: extID}},
			ReleaseDate: releaseDate,
		}
	}

	t.Run("sorts newest-first and normalizes year from release_date", func(t *testing.T) {
		providers := map[domain.ProviderName]ports.ArtistContentProvider{
			domain.ProviderDeezer: &fakeArtistContentProvider{
				getAlbumsFn: func(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
					return []domain.SearchResult{
						album("Older", "2016-09-09", "a1"),
						album("Newest", "2022-01-07", "a3"),
						album("Middle", "2020-03-20", "a2"),
					}, nil
				},
			},
		}
		svc := NewGetArtistContentService(providers)

		resp, err := svc.GetAlbums(context.Background(), domain.ProviderDeezer, "artist-1", "", 0)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		wantOrder := []string{"Newest", "Middle", "Older"}
		wantYear := map[string]int{"Newest": 2022, "Middle": 2020, "Older": 2016}
		if len(resp.Items) != len(wantOrder) {
			t.Fatalf("expected %d items, got %d", len(wantOrder), len(resp.Items))
		}
		for i, want := range wantOrder {
			got := resp.Items[i]
			if got.Title != want {
				t.Errorf("position %d: expected %q, got %q", i, want, got.Title)
			}
			if got.Year != wantYear[want] {
				t.Errorf("%q: expected year %d, got %d", want, wantYear[want], got.Year)
			}
		}
	})

	t.Run("albums with no date sort to the end", func(t *testing.T) {
		providers := map[domain.ProviderName]ports.ArtistContentProvider{
			domain.ProviderDeezer: &fakeArtistContentProvider{
				getAlbumsFn: func(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
					return []domain.SearchResult{
						album("NoDate", "", "a1"),
						album("Dated", "2019-05-01", "a2"),
					}, nil
				},
			},
		}
		svc := NewGetArtistContentService(providers)

		resp, err := svc.GetAlbums(context.Background(), domain.ProviderDeezer, "artist-1", "", 0)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp.Items) != 2 || resp.Items[0].Title != "Dated" || resp.Items[1].Title != "NoDate" {
			t.Errorf("expected [Dated, NoDate], got %v", albumTitles(resp.Items))
		}
	})

	t.Run("limit keeps the newest after sorting", func(t *testing.T) {
		providers := map[domain.ProviderName]ports.ArtistContentProvider{
			domain.ProviderDeezer: &fakeArtistContentProvider{
				getAlbumsFn: func(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
					return []domain.SearchResult{
						album("Old", "2010-01-01", "a1"),
						album("New", "2024-01-01", "a2"),
						album("Mid", "2017-01-01", "a3"),
					}, nil
				},
			},
		}
		svc := NewGetArtistContentService(providers)

		resp, err := svc.GetAlbums(context.Background(), domain.ProviderDeezer, "artist-1", "", 2)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp.Items) != 2 || resp.Items[0].Title != "New" || resp.Items[1].Title != "Mid" {
			t.Errorf("expected [New, Mid], got %v", albumTitles(resp.Items))
		}
	})
}

func albumTitles(items []domain.SearchResult) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.Title)
	}
	return out
}

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

// everyContentProvider lists each real provider, so a fan-out test covers the
// widest identity fan-out production can run.
var everyContentProvider = []domain.ProviderName{
	domain.ProviderDeezer, domain.ProviderMusicBrainz, domain.ProviderSoundCloud,
	domain.ProviderLastFM, domain.ProviderITunes, domain.ProviderTheAudioDB,
	domain.ProviderDiscogs, domain.ProviderYouTube, domain.ProviderAmazonMusic,
	domain.ProviderAppleMusic, domain.ProviderSpotify,
}

// identityFanOut builds a content service over every provider, each with a
// stored ID, whose fetch outcome is decided by fail.
func identityFanOut(fail func(domain.ProviderName) bool, opts ...ArtistContentOption) *GetArtistContentService {
	return identityFanOutWithErr(func(pn domain.ProviderName) error {
		if fail(pn) {
			return upstreamDown
		}
		return nil
	}, opts...)
}

type contentFetcher func(*GetArtistContentService) (*ContentFetchResponse, error)

var identityContentFetchers = map[string]contentFetcher{
	"top tracks": func(s *GetArtistContentService) (*ContentFetchResponse, error) {
		return s.GetTopTracks(context.Background(), domain.ProviderDeezer, "id-deezer", "Che", 10)
	},
	"albums": func(s *GetArtistContentService) (*ContentFetchResponse, error) {
		return s.GetAlbums(context.Background(), domain.ProviderDeezer, "id-deezer", "Che", 50)
	},
}

func TestIdentityFanOut_MostProvidersFailedReportsPartial(t *testing.T) {
	for name, fetch := range identityContentFetchers {
		t.Run(name, func(t *testing.T) {
			svc := identityFanOut(func(pn domain.ProviderName) bool { return pn != domain.ProviderDeezer })

			resp, err := fetch(svc)
			if err != nil {
				t.Fatalf("error = %v", err)
			}
			if resp.Status != domain.ProviderStatusOK || len(resp.Items) != 1 {
				t.Fatalf("status = %s, items = %d, want ok with the one surviving provider's item", resp.Status, len(resp.Items))
			}
			if !resp.Partial {
				t.Errorf("partial = false, want true: %d of %d providers failed", len(everyContentProvider)-1, len(everyContentProvider))
			}
		})
	}
}

func TestIdentityFanOut_AllProvidersAnsweredIsNotPartial(t *testing.T) {
	for name, fetch := range identityContentFetchers {
		t.Run(name, func(t *testing.T) {
			resp, err := fetch(identityFanOut(func(domain.ProviderName) bool { return false }))
			if err != nil {
				t.Fatalf("error = %v", err)
			}
			if len(resp.Items) == 0 || resp.Partial {
				t.Errorf("items = %d, partial = %v, want merged items and partial = false", len(resp.Items), resp.Partial)
			}
		})
	}
}

// A provider the breaker short-circuits never answered, so the merged answer
// is missing its contribution just as if it had errored.
func TestIdentityFanOut_CircuitOpenProviderReportsPartial(t *testing.T) {
	cb := NewCircuitBreaker()
	tripViaSearch(t, cb, domain.ProviderSpotify)
	svc := identityFanOut(func(domain.ProviderName) bool { return false }, WithContentCircuitBreaker(cb))

	resp, err := svc.GetTopTracks(context.Background(), domain.ProviderDeezer, "id-deezer", "Che", 10)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if !resp.Partial {
		t.Error("partial = false, want true (spotify's circuit is open)")
	}
}

// Providers with no stored ID are never asked, so their absence (open circuit
// or not) is not a degradation.
func TestIdentityFanOut_ProviderWithoutIDDoesNotMakePartial(t *testing.T) {
	answer := &fakeArtistContentProvider{
		getTopTracksFn: func(_ context.Context, pn domain.ProviderName, id string) ([]domain.SearchResult, error) {
			return []domain.SearchResult{trackFrom(pn, id, "Real Song", "Che")}, nil
		},
	}
	unreachable := &fakeArtistContentProvider{
		getTopTracksFn: func(context.Context, domain.ProviderName, string) ([]domain.SearchResult, error) {
			t.Error("provider without a stored ID was called")
			return nil, upstreamDown
		},
	}
	cb := NewCircuitBreaker()
	tripViaSearch(t, cb, domain.ProviderYouTube)
	svc := NewGetArtistContentService(
		map[domain.ProviderName]ports.ArtistContentProvider{
			domain.ProviderDeezer:  answer,
			domain.ProviderDiscogs: unreachable,
			domain.ProviderYouTube: unreachable,
		},
		WithContentIdentityStore(&fakeIdentityStore{mbid: "mbid-che", xref: map[string]string{"deezer": "d1"}}),
		WithContentCircuitBreaker(cb),
	)

	resp, err := svc.GetTopTracks(context.Background(), domain.ProviderDeezer, "d1", "Che", 10)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(resp.Items) != 1 || resp.Partial {
		t.Errorf("items = %d, partial = %v, want 1 item and partial = false", len(resp.Items), resp.Partial)
	}
}
