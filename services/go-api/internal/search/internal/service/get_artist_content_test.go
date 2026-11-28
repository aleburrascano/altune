package service_test

import (
	"altune/go-api/internal/search/internal/adapters/memory"
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/internal/ports"
	"altune/go-api/internal/search/internal/service"
	"altune/go-api/internal/search/searchtest"
	"altune/go-api/internal/shared/clock"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestGetArtistContentService_GetTopTracks(t *testing.T) {
	sampleTracks := []domain.SearchResult{
		memory.NewResult(
			domain.ResultKindTrack,
			"Blinding Lights",
			memory.WithSubtitle("The Weeknd"),
			memory.WithSources([]domain.ProviderLink{{Provider: domain.ProviderDeezer, ExternalID: "t1"}}),
		),
		memory.NewResult(
			domain.ResultKindTrack,
			"Save Your Tears",
			memory.WithSubtitle("The Weeknd"),
			memory.WithSources([]domain.ProviderLink{{Provider: domain.ProviderDeezer, ExternalID: "t2"}}),
		),
		memory.NewResult(
			domain.ResultKindTrack,
			"Starboy",
			memory.WithSubtitle("The Weeknd"),
			memory.WithSources([]domain.ProviderLink{{Provider: domain.ProviderDeezer, ExternalID: "t3"}}),
		),
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
				domain.ProviderDeezer: &memory.ArtistContentProvider{
					GetTopTracksFn: func(_ context.Context, pn domain.ProviderName, extID string) ([]domain.SearchResult, error) {
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
				domain.ProviderDeezer: &memory.ArtistContentProvider{
					GetTopTracksFn: func(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
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
				domain.ProviderDeezer: &memory.ArtistContentProvider{
					GetTopTracksFn: func(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
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
			svc := searchtest.NewArtistContent(t, tt.providers)

			resp, err := svc.GetTopTracks(context.Background(), tt.providerName, tt.externalID, nil, tt.limit)
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
				domain.ProviderDeezer: &memory.ArtistContentProvider{
					GetAlbumsFn: func(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
						return []domain.SearchResult{
							memory.NewResult(
								domain.ResultKindAlbum,
								"After Hours",
								memory.WithSources([]domain.ProviderLink{{Provider: domain.ProviderDeezer, ExternalID: "a1"}}),
							),
							memory.NewResult(
								domain.ResultKindAlbum,
								"Starboy",
								memory.WithSources([]domain.ProviderLink{{Provider: domain.ProviderDeezer, ExternalID: "a2"}}),
							),
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
				domain.ProviderDeezer: &memory.ArtistContentProvider{
					GetAlbumsFn: func(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
						return []domain.SearchResult{
							memory.NewResult(
								domain.ResultKindAlbum,
								"After Hours",
								memory.WithSources([]domain.ProviderLink{{Provider: domain.ProviderDeezer, ExternalID: "a1"}}),
								memory.WithTrackCount(14),
							),
							memory.NewResult(
								domain.ResultKindAlbum,
								"After Hours (Deluxe)",
								memory.WithSources([]domain.ProviderLink{{Provider: domain.ProviderDeezer, ExternalID: "a2"}}),
								memory.WithTrackCount(18),
							),
							memory.NewResult(
								domain.ResultKindAlbum,
								"Starboy",
								memory.WithSources([]domain.ProviderLink{{Provider: domain.ProviderDeezer, ExternalID: "a3"}}),
								memory.WithTrackCount(18),
							),
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
				domain.ProviderDeezer: &memory.ArtistContentProvider{
					GetAlbumsFn: func(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
						return []domain.SearchResult{
							memory.NewResult(
								domain.ResultKindAlbum,
								"Dawn FM",
								memory.WithSources([]domain.ProviderLink{{Provider: domain.ProviderDeezer, ExternalID: "a1"}}),
								memory.WithTrackCount(10),
							),
							memory.NewResult(
								domain.ResultKindAlbum,
								"Dawn FM (Alternate World)",
								memory.WithSources([]domain.ProviderLink{{Provider: domain.ProviderDeezer, ExternalID: "a2"}}),
								memory.WithTrackCount(20),
							),
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
				domain.ProviderDeezer: &memory.ArtistContentProvider{
					GetAlbumsFn: func(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
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
			svc := searchtest.NewArtistContent(t, tt.providers)

			resp, err := svc.GetAlbums(context.Background(), tt.providerName, tt.externalID, nil, tt.limit)
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
		return memory.NewResult(
			domain.ResultKindAlbum,
			title,
			memory.WithSources([]domain.ProviderLink{{Provider: domain.ProviderDeezer, ExternalID: extID}}),
			memory.WithReleaseDate(releaseDate),
		)
	}

	t.Run("sorts newest-first and normalizes year from release_date", func(t *testing.T) {
		providers := map[domain.ProviderName]ports.ArtistContentProvider{
			domain.ProviderDeezer: &memory.ArtistContentProvider{
				GetAlbumsFn: func(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
					return []domain.SearchResult{
						album("Older", "2016-09-09", "a1"),
						album("Newest", "2022-01-07", "a3"),
						album("Middle", "2020-03-20", "a2"),
					}, nil
				},
			},
		}
		svc := searchtest.NewArtistContent(t, providers)

		resp, err := svc.GetAlbums(context.Background(), domain.ProviderDeezer, "artist-1", nil, 0)
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
			domain.ProviderDeezer: &memory.ArtistContentProvider{
				GetAlbumsFn: func(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
					return []domain.SearchResult{
						album("NoDate", "", "a1"),
						album("Dated", "2019-05-01", "a2"),
					}, nil
				},
			},
		}
		svc := searchtest.NewArtistContent(t, providers)

		resp, err := svc.GetAlbums(context.Background(), domain.ProviderDeezer, "artist-1", nil, 0)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp.Items) != 2 || resp.Items[0].Title != "Dated" || resp.Items[1].Title != "NoDate" {
			t.Errorf("expected [Dated, NoDate], got %v", memory.AlbumTitles(resp.Items))
		}
	})

	t.Run("limit keeps the newest after sorting", func(t *testing.T) {
		providers := map[domain.ProviderName]ports.ArtistContentProvider{
			domain.ProviderDeezer: &memory.ArtistContentProvider{
				GetAlbumsFn: func(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
					return []domain.SearchResult{
						album("Old", "2010-01-01", "a1"),
						album("New", "2024-01-01", "a2"),
						album("Mid", "2017-01-01", "a3"),
					}, nil
				},
			},
		}
		svc := searchtest.NewArtistContent(t, providers)

		resp, err := svc.GetAlbums(context.Background(), domain.ProviderDeezer, "artist-1", nil, 2)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp.Items) != 2 || resp.Items[0].Title != "New" || resp.Items[1].Title != "Mid" {
			t.Errorf("expected [New, Mid], got %v", memory.AlbumTitles(resp.Items))
		}
	})
}

const fanOutFailedEvent = "artist_content.fanout.provider_failed"

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

func TestIdentityFanOut_ProviderFailuresWarnOnceAtProductionLevel(t *testing.T) {
	const secret = "0123456789abcdef0123456789abcdef"
	leaky := &url.Error{Op: "Get", URL: "https://ws.audioscrobbler.com/2.0/?api_key=" + secret, Err: errors.New("connection refused")}
	for name, fetch := range identityContentFetchers {
		t.Run(name, func(t *testing.T) {
			svc := identityFanOutWithErr(t, func(pn domain.ProviderName) error {
				if pn == domain.ProviderTheAudioDB || pn == domain.ProviderSpotify {
					return leaky
				}
				return nil
			})
			buf := memory.CaptureProductionLogs(t)

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
				t.Fatalf("failed = %v, want exactly theaudiodb and spotify", rec["failed"])
			}
			for _, pn := range []domain.ProviderName{domain.ProviderTheAudioDB, domain.ProviderSpotify} {
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
	svc := identityFanOutWithErr(t, func(domain.ProviderName) error { return nil })
	buf := memory.CaptureProductionLogs(t)

	if _, err := svc.GetTopTracks(context.Background(), domain.ProviderDeezer, "id-deezer", nil, 10); err != nil {
		t.Fatalf("error = %v", err)
	}
	if recs := fanOutFailureRecords(t, buf); len(recs) != 0 {
		t.Errorf("got %d failure records, want none:\n%s", len(recs), buf)
	}
}

func TestIdentityFanOut_NoFailureWarnWhenCallerCancels(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc := identityFanOutWithErr(t, func(pn domain.ProviderName) error {
		if pn == domain.ProviderDeezer {
			return nil
		}
		cancel()
		return context.Canceled
	})
	buf := memory.CaptureProductionLogs(t)

	if _, err := svc.GetTopTracks(ctx, domain.ProviderDeezer, "id-deezer", nil, 10); err != nil {
		t.Fatalf("error = %v", err)
	}
	if recs := fanOutFailureRecords(t, buf); len(recs) != 0 {
		t.Errorf("got %d failure records for a cancelled request, want none:\n%s", len(recs), buf)
	}
}

func TestIdentityFanOut_NoFailureWarnForCircuitOpenSkip(t *testing.T) {
	cb := service.NewCircuitBreaker(clock.Now)
	tripCircuit(t, cb, domain.ProviderSpotify)
	svc := identityFanOutWithErr(t, func(domain.ProviderName) error { return nil }, searchtest.ArtistBreaker(cb))
	buf := memory.CaptureProductionLogs(t)

	if _, err := svc.GetTopTracks(context.Background(), domain.ProviderDeezer, "id-deezer", nil, 10); err != nil {
		t.Fatalf("error = %v", err)
	}
	if recs := fanOutFailureRecords(t, buf); len(recs) != 0 {
		t.Errorf("got %d failure records for a circuit-open skip, want none:\n%s", len(recs), buf)
	}
}

func identityFanOutWithErr(t testing.TB, errFor func(domain.ProviderName) error, opts ...searchtest.ArtistContentPart) *service.GetArtistContentService {
	providers := make(map[domain.ProviderName]ports.ArtistContentProvider, len(everyContentProvider))
	xref := make(map[domain.ProviderKey]string, len(everyContentProvider))
	for _, name := range everyContentProvider {
		xref[name.Key()] = "id-" + name.String()
		providers[name] = &memory.ArtistContentProvider{
			GetTopTracksFn: func(_ context.Context, pn domain.ProviderName, id string) ([]domain.SearchResult, error) {
				if err := errFor(pn); err != nil {
					return nil, err
				}
				return []domain.SearchResult{memory.TrackFrom(pn, id, "Real Song", "Che")}, nil
			},
			GetAlbumsFn: func(_ context.Context, pn domain.ProviderName, id string) ([]domain.SearchResult, error) {
				if err := errFor(pn); err != nil {
					return nil, err
				}
				return []domain.SearchResult{memory.V2Album(pn, id, "Fully Loaded", withDate("2026-04-01"))}, nil
			},
		}
	}
	store := &memory.FakeIdentityStore{Mbid: "mbid-che", Xref: xref}
	return searchtest.NewArtistContent(t, providers, append(opts, searchtest.ArtistIdentities(store))...)
}

var everyContentProvider = []domain.ProviderName{
	domain.ProviderDeezer, domain.ProviderMusicBrainz, domain.ProviderSoundCloud,
	domain.ProviderITunes, domain.ProviderTheAudioDB,
	domain.ProviderDiscogs, domain.ProviderYouTube,
	domain.ProviderAppleMusic, domain.ProviderSpotify,
}

func identityFanOut(t testing.TB, fail func(domain.ProviderName) bool, opts ...searchtest.ArtistContentPart) *service.GetArtistContentService {
	return identityFanOutWithErr(t, func(pn domain.ProviderName) error {
		if fail(pn) {
			return memory.ErrUpstreamDown
		}
		return nil
	}, opts...)
}

type contentFetcher func(*service.GetArtistContentService) (*service.ContentFetchResponse, error)

var identityContentFetchers = map[string]contentFetcher{
	"top tracks": func(s *service.GetArtistContentService) (*service.ContentFetchResponse, error) {
		return s.GetTopTracks(context.Background(), domain.ProviderDeezer, "id-deezer", nil, 10)
	},
	"albums": func(s *service.GetArtistContentService) (*service.ContentFetchResponse, error) {
		return s.GetAlbums(context.Background(), domain.ProviderDeezer, "id-deezer", nil, 50)
	},
}

func TestIdentityFanOut_MostProvidersFailedReportsPartial(t *testing.T) {
	for name, fetch := range identityContentFetchers {
		t.Run(name, func(t *testing.T) {
			svc := identityFanOut(t, func(pn domain.ProviderName) bool { return pn != domain.ProviderDeezer })

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
			resp, err := fetch(identityFanOut(t, func(domain.ProviderName) bool { return false }))
			if err != nil {
				t.Fatalf("error = %v", err)
			}
			if len(resp.Items) == 0 || resp.Partial {
				t.Errorf("items = %d, partial = %v, want merged items and partial = false", len(resp.Items), resp.Partial)
			}
		})
	}
}

func TestIdentityFanOut_CircuitOpenProviderReportsPartial(t *testing.T) {
	cb := service.NewCircuitBreaker(clock.Now)
	tripCircuit(t, cb, domain.ProviderSpotify)
	svc := identityFanOut(t, func(domain.ProviderName) bool { return false }, searchtest.ArtistBreaker(cb))

	resp, err := svc.GetTopTracks(context.Background(), domain.ProviderDeezer, "id-deezer", nil, 10)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if !resp.Partial {
		t.Error("partial = false, want true (spotify's circuit is open)")
	}
}

func TestIdentityFanOut_ProviderWithoutIDDoesNotMakePartial(t *testing.T) {
	answer := &memory.ArtistContentProvider{
		GetTopTracksFn: func(_ context.Context, pn domain.ProviderName, id string) ([]domain.SearchResult, error) {
			return []domain.SearchResult{memory.TrackFrom(pn, id, "Real Song", "Che")}, nil
		},
	}
	unreachable := &memory.ArtistContentProvider{
		GetTopTracksFn: func(context.Context, domain.ProviderName, string) ([]domain.SearchResult, error) {
			t.Error("provider without a stored ID was called")
			return nil, memory.ErrUpstreamDown
		},
	}
	cb := service.NewCircuitBreaker(clock.Now)
	tripCircuit(t, cb, domain.ProviderYouTube)
	svc := searchtest.NewArtistContent(t, map[domain.ProviderName]ports.ArtistContentProvider{
		domain.ProviderDeezer:  answer,
		domain.ProviderDiscogs: unreachable,
		domain.ProviderYouTube: unreachable,
	}, searchtest.ArtistIdentities(&memory.FakeIdentityStore{Mbid: "mbid-che", Xref: map[domain.ProviderKey]string{"deezer": "d1"}}), searchtest.ArtistBreaker(cb))

	resp, err := svc.GetTopTracks(context.Background(), domain.ProviderDeezer, "d1", nil, 10)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(resp.Items) != 1 || resp.Partial {
		t.Errorf("items = %d, partial = %v, want 1 item and partial = false", len(resp.Items), resp.Partial)
	}
}

func TestGetTopTracks_PanickingProviderIsContained(t *testing.T) {
	svc := searchtest.NewArtistContent(t, map[domain.ProviderName]ports.ArtistContentProvider{
		domain.ProviderDeezer: &memory.ArtistContentProvider{
			GetTopTracksFn: func(context.Context, domain.ProviderName, string) ([]domain.SearchResult, error) {
				panic("deezer exploded")
			},
		},
		domain.ProviderITunes: &memory.ArtistContentProvider{
			GetTopTracksFn: func(_ context.Context, provider domain.ProviderName, id string) ([]domain.SearchResult, error) {
				return []domain.SearchResult{memory.TrackFrom(provider, id, "Track", "Artist")}, nil
			},
		},
	})
	known := map[domain.ProviderName]string{domain.ProviderDeezer: "dz", domain.ProviderITunes: "it"}

	resp, err := svc.GetTopTracks(context.Background(), domain.ProviderDeezer, "dz", known, 10)
	if err != nil {
		t.Fatalf("error = %v", err)
	}

	if len(resp.Items) != 1 {
		t.Fatalf("items = %d, want 1 (the non-panicking provider's track)", len(resp.Items))
	}
	if !resp.Partial {
		t.Error("partial = false, want true (the panicking provider failed to answer)")
	}
}

func TestGetArtistContent_LaterPageFailureReportsPartial(t *testing.T) {
	truncated := &domain.PartialResultError{Page: 2, Err: memory.ErrUpstreamDown}
	for name, fetch := range identityContentFetchers {
		t.Run("identity fan-out "+name, func(t *testing.T) {
			svc := identityFanOutWithErr(t, func(pn domain.ProviderName) error {
				if pn == domain.ProviderDeezer {
					return truncated
				}
				return nil
			})
			resp, err := fetch(svc)
			if err != nil {
				t.Fatalf("error = %v", err)
			}
			if resp.Status != domain.ProviderStatusOK || len(resp.Items) == 0 {
				t.Fatalf("status = %s, items = %d, want ok with the merged items", resp.Status, len(resp.Items))
			}
			if !resp.Partial {
				t.Error("partial = false, want true: a provider's later page failed")
			}
		})
	}

	t.Run("single provider albums", func(t *testing.T) {
		svc := searchtest.NewArtistContent(t, map[domain.ProviderName]ports.ArtistContentProvider{
			domain.ProviderDeezer: &memory.ArtistContentProvider{
				GetAlbumsFn: func(_ context.Context, pn domain.ProviderName, id string) ([]domain.SearchResult, error) {
					return []domain.SearchResult{memory.V2Album(pn, id, "Fully Loaded", withDate("2026-04-01"))}, truncated
				},
			},
		})
		resp, err := svc.GetAlbums(context.Background(), domain.ProviderDeezer, "id-deezer", nil, 50)
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if resp.Status != domain.ProviderStatusOK || len(resp.Items) != 1 || !resp.Partial {
			t.Errorf("status = %s, items = %d, partial = %v, want ok, the page-0 item, partial", resp.Status, len(resp.Items), resp.Partial)
		}
	})
}

const artistBreakerTripsAfter = 5

func tripCircuit(t *testing.T, cb *service.CircuitBreaker, provider domain.ProviderName) {
	t.Helper()
	for range artistBreakerTripsAfter {
		cb.RecordFailure(context.Background(), provider, service.Admission{})
	}
	if cb.GetStatus(provider) != domain.ProviderStatusCircuitOpen {
		t.Fatalf("setup: %s circuit not open after %d failures", provider, artistBreakerTripsAfter)
	}
}

func withDate(d string) func(*domain.SearchResult) {
	return func(r *domain.SearchResult) { r.ReleaseDate = d }
}

func withTracks(n int) func(*domain.SearchResult) {
	return func(r *domain.SearchResult) { r.TrackCount = n }
}

func withCover(u string) func(*domain.SearchResult) {
	return func(r *domain.SearchResult) { r.ImageURL = u }
}

func withType(t domain.RecordType) func(*domain.SearchResult) {
	return func(r *domain.SearchResult) { r.RecordType = t }
}

func TestGetAlbums_v2_bestOfMerge(t *testing.T) {
	deezer := &memory.ArtistContentProvider{
		GetAlbumsFn: func(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
			return []domain.SearchResult{
				memory.V2Album(domain.ProviderDeezer, "d-fl", "Fully Loaded",
					withDate("2026-04-01"), withCover("cover-dz"), withType("ep")),
			}, nil
		},
	}
	itunes := &memory.ArtistContentProvider{
		GetAlbumsFn: func(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
			return []domain.SearchResult{
				memory.V2Album(domain.ProviderITunes, "i-fl", "Fully Loaded", withTracks(5)),
			}, nil
		},
	}
	store := &memory.FakeIdentityStore{Mbid: "mbid-che", Xref: map[domain.ProviderKey]string{"deezer": "d1", "itunes": "i1"}}
	svc := searchtest.NewArtistContent(t, map[domain.ProviderName]ports.ArtistContentProvider{
		domain.ProviderDeezer: deezer,
		domain.ProviderITunes: itunes,
	}, searchtest.ArtistIdentities(store))

	resp, err := svc.GetAlbums(context.Background(), domain.ProviderDeezer, "d1", nil, 50)
	if err != nil {
		t.Fatalf("GetAlbums error = %v", err)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("items = %d, want 1 (Fully Loaded, merged across Deezer and iTunes): %+v", len(resp.Items), resp.Items)
	}
	a := resp.Items[0]
	if a.Title != "Fully Loaded" {
		t.Fatalf("title = %q, want Fully Loaded", a.Title)
	}
	if a.Year != 2026 {
		t.Errorf("Year = %d, want 2026 (normalized from Deezer's date)", a.Year)
	}
	if a.TrackCount != 5 {
		t.Errorf("TrackCount = %d, want 5 (best-of from iTunes)", a.TrackCount)
	}
	if a.ImageURL != "cover-dz" {
		t.Errorf("ImageURL = %q, want cover-dz (best-of from Deezer)", a.ImageURL)
	}
	if rt := a.RecordType; rt != "ep" {
		t.Errorf("record_type = %q, want ep", rt)
	}
	if len(a.Sources) != 2 {
		t.Errorf("sources = %d, want 2 (deezer + itunes unioned)", len(a.Sources))
	}
}

func TestGetAlbums_v2_runsOnSeedWhenStoreHasNoBridge(t *testing.T) {
	deezer := &memory.ArtistContentProvider{
		GetAlbumsFn: func(_ context.Context, _ domain.ProviderName, id string) ([]domain.SearchResult, error) {
			if id != "399574001" {
				t.Errorf("deezer queried id %q, want the seed 399574001", id)
			}
			return []domain.SearchResult{
				memory.V2Album(domain.ProviderDeezer, "d-rib", "REST IN BASS: ENCORE", withDate("2025-12-25"), withCover("c")),
			}, nil
		},
	}
	svc := searchtest.NewArtistContent(t, map[domain.ProviderName]ports.ArtistContentProvider{domain.ProviderDeezer: deezer}, searchtest.ArtistIdentities(&memory.FakeIdentityStore{}))

	resp, err := svc.GetAlbums(context.Background(), domain.ProviderDeezer, "399574001", nil, 50)
	if err != nil {
		t.Fatalf("GetAlbums error = %v", err)
	}
	if len(resp.Items) != 1 || resp.Items[0].Title != "REST IN BASS: ENCORE" {
		t.Fatalf("items = %+v, want just the id-verified ENCORE (V2 ran on seed)", resp.Items)
	}
}

func TestGetAlbums_v2_deterministicAcrossRuns(t *testing.T) {
	deezer := &memory.ArtistContentProvider{
		GetAlbumsFn: func(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
			return []domain.SearchResult{
				memory.V2Album(domain.ProviderDeezer, "d-fl", "Fully Loaded", withDate("2026-04-01")),
			}, nil
		},
	}
	itunes := &memory.ArtistContentProvider{
		GetAlbumsFn: func(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
			return []domain.SearchResult{
				memory.V2Album(domain.ProviderITunes, "i-fl", "FULLY LOADED", withTracks(5)),
			}, nil
		},
	}
	store := &memory.FakeIdentityStore{Mbid: "mbid-che", Xref: map[domain.ProviderKey]string{"deezer": "d1", "itunes": "i1"}}
	svc := searchtest.NewArtistContent(t, map[domain.ProviderName]ports.ArtistContentProvider{
		domain.ProviderDeezer: deezer,
		domain.ProviderITunes: itunes,
	}, searchtest.ArtistIdentities(store))

	for i := 0; i < 25; i++ {
		resp, err := svc.GetAlbums(context.Background(), domain.ProviderDeezer, "d1", nil, 50)
		if err != nil {
			t.Fatalf("run %d: GetAlbums error = %v", i, err)
		}
		if len(resp.Items) != 1 {
			t.Fatalf("run %d: items = %d, want 1 merged", i, len(resp.Items))
		}
		if got := resp.Items[0].Title; got != "Fully Loaded" {
			t.Fatalf("run %d: title = %q, want %q (Deezer is the canonical incumbent every run)", i, got, "Fully Loaded")
		}
	}
}

func failOnceThenEmptyProvider() *memory.ArtistContentProvider {
	var albumCalls, trackCalls int
	return &memory.ArtistContentProvider{
		GetAlbumsFn: func(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
			albumCalls++
			if albumCalls == 1 {
				return nil, errors.New("provider down")
			}
			return nil, nil
		},
		GetTopTracksFn: func(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
			trackCalls++
			if trackCalls == 1 {
				return nil, errors.New("provider down")
			}
			return nil, nil
		},
	}
}

func fallbackPartialService(t testing.TB, p *memory.ArtistContentProvider) *service.GetArtistContentService {
	store := &memory.FakeIdentityStore{Mbid: "mbid-che", Xref: map[domain.ProviderKey]string{"deezer": "d1"}}
	return searchtest.NewArtistContent(t, map[domain.ProviderName]ports.ArtistContentProvider{domain.ProviderDeezer: p}, searchtest.ArtistIdentities(store))
}

func TestGetAlbums_v2_fallbackKeepsPartialWhenIdentityFanOutFailed(t *testing.T) {
	resp, err := fallbackPartialService(t, failOnceThenEmptyProvider()).GetAlbums(context.Background(), domain.ProviderDeezer, "d1", nil, 50)
	if err != nil {
		t.Fatalf("GetAlbums error = %v", err)
	}
	if !resp.Partial {
		t.Fatalf("Partial = false, want true after v2 failed with zero results")
	}
}

func TestGetTopTracks_v2_fallbackKeepsPartialWhenIdentityFanOutFailed(t *testing.T) {
	resp, err := fallbackPartialService(t, failOnceThenEmptyProvider()).GetTopTracks(context.Background(), domain.ProviderDeezer, "d1", nil, 10)
	if err != nil {
		t.Fatalf("GetTopTracks error = %v", err)
	}
	if !resp.Partial {
		t.Fatalf("Partial = false, want true after v2 failed with zero results")
	}
}

func TestGetArtistContent_v2_fallbackNotPartialWhenNoProviderFailed(t *testing.T) {
	svc := fallbackPartialService(t, &memory.ArtistContentProvider{
		GetAlbumsFn: func(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
			return nil, nil
		},
		GetTopTracksFn: func(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
			return nil, nil
		},
	})
	albums, err := svc.GetAlbums(context.Background(), domain.ProviderDeezer, "d1", nil, 50)
	if err != nil {
		t.Fatalf("GetAlbums error = %v", err)
	}
	if albums.Partial {
		t.Errorf("GetAlbums Partial = true, want false when no provider failed")
	}
	tracks, err := svc.GetTopTracks(context.Background(), domain.ProviderDeezer, "d1", nil, 10)
	if err != nil {
		t.Fatalf("GetTopTracks error = %v", err)
	}
	if tracks.Partial {
		t.Errorf("GetTopTracks Partial = true, want false when no provider failed")
	}
}

func TestGetTopTracks_v2_corroboratedFirst(t *testing.T) {
	deezer := &memory.ArtistContentProvider{
		GetTopTracksFn: func(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
			return []domain.SearchResult{
				memory.TrackFrom(domain.ProviderDeezer, "d-real", "Real Song", "Che"),
				memory.TrackFrom(domain.ProviderDeezer, "d-solo", "Solo", "Che"),
			}, nil
		},
	}
	itunes := &memory.ArtistContentProvider{
		GetTopTracksFn: func(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
			return []domain.SearchResult{memory.TrackFrom(domain.ProviderITunes, "i-real", "Real Song", "Che")}, nil
		},
	}
	store := &memory.FakeIdentityStore{Mbid: "mbid-che", Xref: map[domain.ProviderKey]string{"deezer": "d1", "itunes": "i1"}}
	svc := searchtest.NewArtistContent(t, map[domain.ProviderName]ports.ArtistContentProvider{
		domain.ProviderDeezer: deezer,
		domain.ProviderITunes: itunes,
	}, searchtest.ArtistIdentities(store))

	resp, err := svc.GetTopTracks(context.Background(), domain.ProviderDeezer, "d1", nil, 10)
	if err != nil {
		t.Fatalf("GetTopTracks error = %v", err)
	}
	if len(resp.Items) != 2 {
		t.Fatalf("items = %d, want 2 merged", len(resp.Items))
	}
	if resp.Items[0].Title != "Real Song" {
		t.Errorf("first = %q, want Real Song (2-provider corroboration first)", resp.Items[0].Title)
	}
}

type artistContentArgs struct {
	providers   map[domain.ProviderName]ports.ArtistContentProvider
	tracklists  map[domain.ProviderName]ports.AlbumContentProvider
	identity    service.ArtistIdentity
	anchoring   service.Anchoring
	discography service.DiscographyObserver
	breaker     *service.CircuitBreaker
}

func completeArtistContentArgs() artistContentArgs {
	return artistContentArgs{
		providers:   map[domain.ProviderName]ports.ArtistContentProvider{},
		tracklists:  map[domain.ProviderName]ports.AlbumContentProvider{},
		identity:    service.SeedPathIdentity{},
		anchoring:   service.NoAnchoring{},
		discography: service.NoDiscographyObserver{},
		breaker:     service.NewCircuitBreaker(clock.Now),
	}
}

func (a artistContentArgs) build() (*service.GetArtistContentService, error) {
	return service.NewGetArtistContentService(a.providers, a.tracklists, a.identity, a.anchoring, a.discography, a.breaker)
}

func TestNewGetArtistContentService_RefusesEveryMissingCollaborator(t *testing.T) {
	if _, err := completeArtistContentArgs().build(); err != nil {
		t.Fatalf("complete collaborators refused: %v", err)
	}
	cases := map[string]struct {
		without func(*artistContentArgs)
		named   string
	}{
		"providers":            {func(a *artistContentArgs) { a.providers = nil }, "providers"},
		"tracklist providers":  {func(a *artistContentArgs) { a.tracklists = nil }, "tracklist providers"},
		"identity":             {func(a *artistContentArgs) { a.identity = nil }, "artist identity"},
		"anchoring":            {func(a *artistContentArgs) { a.anchoring = nil }, "anchoring strategy"},
		"discography observer": {func(a *artistContentArgs) { a.discography = nil }, "discography observer"},
		"breaker":              {func(a *artistContentArgs) { a.breaker = nil }, "circuit breaker"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			args := completeArtistContentArgs()
			tc.without(&args)

			svc, err := args.build()

			if !errors.Is(err, domain.ErrArtistPageCollaboratorRequired) || svc != nil {
				t.Fatalf("svc = %v, err = %v, want artistpage.ErrCollaboratorRequired and no service", svc, err)
			}
			if !strings.Contains(err.Error(), tc.named) {
				t.Errorf("err = %q, want it to name %q", err, tc.named)
			}
		})
	}
}

func TestNewMusicBrainzAnchoring_RefusesEveryMissingCollaborator(t *testing.T) {
	if _, err := service.NewMusicBrainzAnchoring(stubMBAnchor{}, nil); !errors.Is(err, domain.ErrArtistPageCollaboratorRequired) || !strings.Contains(err.Error(), "circuit breaker") {
		t.Errorf("without a breaker: err = %v, want artistpage.ErrCollaboratorRequired naming the circuit breaker", err)
	}
	if _, err := service.NewMusicBrainzAnchoring(nil, service.NewCircuitBreaker(clock.Now)); !errors.Is(err, domain.ErrArtistPageCollaboratorRequired) || !strings.Contains(err.Error(), "discography anchor") {
		t.Errorf("without an anchor: err = %v, want artistpage.ErrCollaboratorRequired naming the discography anchor", err)
	}
}

func TestNewDiscographyTelemetry_RefusesEveryMissingCollaborator(t *testing.T) {
	if _, err := service.NewDiscographyTelemetry(nil, searchtest.NewTracker(t), clock.Now); !errors.Is(err, domain.ErrArtistPageCollaboratorRequired) || !strings.Contains(err.Error(), "event store") {
		t.Errorf("without an event store: err = %v, want artistpage.ErrCollaboratorRequired naming the event store", err)
	}
	if _, err := service.NewDiscographyTelemetry(&searchtest.FakeEventStore{}, nil, clock.Now); !errors.Is(err, domain.ErrArtistPageCollaboratorRequired) || !strings.Contains(err.Error(), "background tracker") {
		t.Errorf("without a tracker: err = %v, want artistpage.ErrCollaboratorRequired naming the background tracker", err)
	}
}

func TestNoAnchoring_LeavesTheAnswerAndTheBreakerUntouched(t *testing.T) {
	deezer := &memory.ArtistContentProvider{
		GetAlbumsFn: func(context.Context, domain.ProviderName, string) ([]domain.SearchResult, error) {
			return []domain.SearchResult{memory.V2Album(domain.ProviderDeezer, "d-fl", "Fully Loaded", withDate("2026-04-01"))}, nil
		},
		GetTopTracksFn: func(_ context.Context, pn domain.ProviderName, id string) ([]domain.SearchResult, error) {
			return []domain.SearchResult{memory.TrackFrom(pn, id, "Real Song", "Che")}, nil
		},
	}
	identity, err := service.NewStoreIdentity(&memory.FakeIdentityStore{Mbid: "mbid-che", Xref: map[domain.ProviderKey]string{"deezer": "d1"}})
	if err != nil {
		t.Fatalf("NewStoreIdentity: %v", err)
	}
	breaker := service.NewCircuitBreaker(clock.Now)
	before := breaker.Snapshot(domain.ProviderMusicBrainz)
	svc, err := service.NewGetArtistContentService(
		map[domain.ProviderName]ports.ArtistContentProvider{domain.ProviderDeezer: deezer},
		map[domain.ProviderName]ports.AlbumContentProvider{},
		identity, service.NoAnchoring{}, service.NoDiscographyObserver{}, breaker)
	if err != nil {
		t.Fatalf("NewGetArtistContentService: %v", err)
	}

	for name, fetch := range identityContentFetchers {
		t.Run(name, func(t *testing.T) {
			resp, err := fetch(svc)
			if err != nil {
				t.Fatalf("error = %v", err)
			}
			if len(resp.Items) != 1 || resp.Partial {
				t.Errorf("items = %d, partial = %v, want the provider's one item and partial = false", len(resp.Items), resp.Partial)
			}
		})
	}
	if after := breaker.Snapshot(domain.ProviderMusicBrainz); after != before {
		t.Errorf("MusicBrainz circuit = %+v after the fetches, want %+v: no anchor call may be recorded", after, before)
	}
}

type stubMBAnchor struct {
	titles []string
	err    error
}

func (a stubMBAnchor) ReleaseGroupTitles(context.Context, string) ([]string, error) {
	return a.titles, a.err
}

func anchoredAlbumsService(t testing.TB, anchor stubMBAnchor, extra ...searchtest.ArtistContentPart) *service.GetArtistContentService {
	deezer := &memory.ArtistContentProvider{
		GetAlbumsFn: func(context.Context, domain.ProviderName, string) ([]domain.SearchResult, error) {
			return []domain.SearchResult{memory.V2Album(domain.ProviderDeezer, "d-fl", "Fully Loaded", withDate("2026-04-01"))}, nil
		},
	}
	opts := append([]searchtest.ArtistContentPart{
		searchtest.ArtistIdentities(&memory.FakeIdentityStore{Mbid: "mbid-che", Xref: map[domain.ProviderKey]string{"deezer": "d1"}}),
		searchtest.ArtistAnchor(anchor),
	}, extra...)
	return searchtest.NewArtistContent(t, map[domain.ProviderName]ports.ArtistContentProvider{domain.ProviderDeezer: deezer}, opts...)
}

func anchorFailedRecords(t *testing.T, buf *bytes.Buffer) []map[string]any {
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
		if rec["msg"] == "artist_albums.mb_anchor_failed" {
			out = append(out, rec)
		}
	}
	return out
}

func TestGetAlbums_v2_anchorErrorShipsUnfilteredAlbumsAsPartialWithOneWarn(t *testing.T) {
	const secret = "0123456789abcdef0123456789abcdef"
	leaky := &url.Error{Op: "Get", URL: "https://musicbrainz.org/ws/2/?api_key=" + secret, Err: errors.New("connection refused")}
	openBreaker := service.NewCircuitBreaker(clock.Now)
	for range artistBreakerTripsAfter {
		openBreaker.RecordFailure(context.Background(), domain.ProviderMusicBrainz, service.Admission{})
	}
	cases := map[string]*service.GetArtistContentService{
		"anchor error": anchoredAlbumsService(t, stubMBAnchor{err: leaky}),
		"breaker open": anchoredAlbumsService(t, stubMBAnchor{titles: []string{"Fully Loaded"}}, searchtest.ArtistBreaker(openBreaker)),
	}
	for name, svc := range cases {
		t.Run(name, func(t *testing.T) {
			buf := memory.CaptureProductionLogs(t)
			resp, err := svc.GetAlbums(context.Background(), domain.ProviderDeezer, "d1", nil, 50)
			if err != nil {
				t.Fatalf("GetAlbums error = %v", err)
			}
			if len(resp.Items) != 1 || !resp.Partial {
				t.Fatalf("items = %d, partial = %v, want 1 unfiltered item and partial = true", len(resp.Items), resp.Partial)
			}
			recs := anchorFailedRecords(t, buf)
			if len(recs) != 1 || recs[0]["level"] != "WARN" || recs[0]["mbid"] != "mbid-che" {
				t.Fatalf("records = %v, want one WARN carrying mbid-che", recs)
			}
			if strings.Contains(buf.String(), secret) {
				t.Errorf("log leaked the secret: %s", buf.String())
			}
		})
	}
}

func TestGetAlbums_v2_anchorWithNoTitlesIsSilentNoOp(t *testing.T) {
	buf := memory.CaptureProductionLogs(t)
	resp, err := anchoredAlbumsService(t, stubMBAnchor{}).GetAlbums(context.Background(), domain.ProviderDeezer, "d1", nil, 50)
	if err != nil {
		t.Fatalf("GetAlbums error = %v", err)
	}
	if len(resp.Items) != 1 || resp.Partial {
		t.Errorf("items = %d, partial = %v, want 1 item and partial = false", len(resp.Items), resp.Partial)
	}
	if recs := anchorFailedRecords(t, buf); len(recs) != 0 {
		t.Errorf("records = %v, want none", recs)
	}
}

func TestGetAlbums_v2_anchorErrorAfterCallerLeftIsSilent(t *testing.T) {
	buf := memory.CaptureProductionLogs(t)
	ctx, cancel := context.WithCancel(context.Background())
	svc := anchoredAlbumsService(t, stubMBAnchor{err: context.Canceled})
	cancel()
	resp, err := svc.GetAlbums(ctx, domain.ProviderDeezer, "d1", nil, 50)
	if err != nil {
		t.Fatalf("GetAlbums error = %v", err)
	}
	if resp.Partial && len(resp.Items) > 0 {
		t.Errorf("Partial = true with items, want the anchor failure not to set it")
	}
	if recs := anchorFailedRecords(t, buf); len(recs) != 0 {
		t.Errorf("records = %v, want none", recs)
	}
}

type funcMBAnchor func(ctx context.Context, mbid string) ([]string, error)

func (f funcMBAnchor) ReleaseGroupTitles(ctx context.Context, mbid string) ([]string, error) {
	return f(ctx, mbid)
}

func anchoredAlbumsServiceWith(t testing.TB, anchor ports.MusicBrainzDiscographyAnchor, extra ...searchtest.ArtistContentPart) *service.GetArtistContentService {
	deezer := &memory.ArtistContentProvider{
		GetAlbumsFn: func(context.Context, domain.ProviderName, string) ([]domain.SearchResult, error) {
			return []domain.SearchResult{memory.V2Album(domain.ProviderDeezer, "d-fl", "Fully Loaded", withDate("2026-04-01"))}, nil
		},
	}
	opts := append([]searchtest.ArtistContentPart{
		searchtest.ArtistIdentities(&memory.FakeIdentityStore{Mbid: "mbid-che", Xref: map[domain.ProviderKey]string{"deezer": "d1"}}),
		searchtest.ArtistAnchor(anchor),
	}, extra...)
	return searchtest.NewArtistContent(t, map[domain.ProviderName]ports.ArtistContentProvider{domain.ProviderDeezer: deezer}, opts...)
}

func TestGetAlbums_v2_callerLeavingDuringAnchorLookupIsSilentAndNotPartial(t *testing.T) {
	buf := memory.CaptureProductionLogs(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	anchorCalled := false
	svc := anchoredAlbumsServiceWith(t, funcMBAnchor(func(context.Context, string) ([]string, error) {
		anchorCalled = true
		cancel()
		return nil, context.Canceled
	}))
	resp, err := svc.GetAlbums(ctx, domain.ProviderDeezer, "d1", nil, 50)
	if !anchorCalled {
		t.Fatalf("anchor was never consulted, the test did not reach the anchor path")
	}
	if err != nil {
		t.Fatalf("GetAlbums error = %v", err)
	}
	if len(resp.Items) == 0 {
		t.Fatalf("items = 0, want the provider's album so Partial is checked with items present")
	}
	if resp.Partial {
		t.Errorf("Partial = true with %d items, want false when the caller left before the anchor failed", len(resp.Items))
	}
	if recs := anchorFailedRecords(t, buf); len(recs) != 0 {
		t.Errorf("records = %v, want none after the caller left", recs)
	}
}

func TestGetAlbums_v2_callerDeadlinePassingDuringAnchorLookupIsSilent(t *testing.T) {
	buf := memory.CaptureProductionLogs(t)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	anchorCalled := false
	svc := anchoredAlbumsServiceWith(t, funcMBAnchor(func(context.Context, string) ([]string, error) {
		anchorCalled = true
		<-ctx.Done()
		return nil, ctx.Err()
	}))
	resp, err := svc.GetAlbums(ctx, domain.ProviderDeezer, "d1", nil, 50)
	if !anchorCalled {
		t.Fatalf("anchor was never consulted, the test did not reach the anchor path")
	}
	if err != nil {
		t.Fatalf("GetAlbums error = %v", err)
	}
	if len(resp.Items) == 0 {
		t.Fatalf("items = 0, want the provider's album so Partial is checked with items present")
	}
	if resp.Partial {
		t.Errorf("Partial = true with items, want false when the caller's own deadline passed")
	}
	if recs := anchorFailedRecords(t, buf); len(recs) != 0 {
		t.Errorf("records = %v, want none after the caller's deadline passed", recs)
	}
}

func TestGetAlbums_v2_anchorTimeoutWhileCallerStillWaitingIsPartialWithOneWarn(t *testing.T) {
	errs := map[string]error{
		"deadline exceeded":         context.DeadlineExceeded,
		"wrapped deadline exceeded": errors.Join(errors.New("mb flight"), context.DeadlineExceeded),
		"anchor's own cancel":       context.Canceled,
	}
	for name, anchorErr := range errs {
		t.Run(name, func(t *testing.T) {
			buf := memory.CaptureProductionLogs(t)
			svc := anchoredAlbumsServiceWith(t, funcMBAnchor(func(context.Context, string) ([]string, error) {
				return nil, anchorErr
			}))
			resp, err := svc.GetAlbums(context.Background(), domain.ProviderDeezer, "d1", nil, 50)
			if err != nil {
				t.Fatalf("GetAlbums error = %v", err)
			}
			if len(resp.Items) != 1 || !resp.Partial {
				t.Fatalf("items = %d, partial = %v, want 1 unfiltered item and partial = true", len(resp.Items), resp.Partial)
			}
			recs := anchorFailedRecords(t, buf)
			if len(recs) != 1 || recs[0]["level"] != "WARN" || recs[0]["mbid"] != "mbid-che" {
				t.Fatalf("records = %v, want one WARN carrying mbid-che", recs)
			}
		})
	}
}

func TestGetAlbums_v2_openBreakerAndFailingAnchorWarnOnce(t *testing.T) {
	buf := memory.CaptureProductionLogs(t)
	openBreaker := service.NewCircuitBreaker(clock.Now)
	for range artistBreakerTripsAfter {
		openBreaker.RecordFailure(context.Background(), domain.ProviderMusicBrainz, service.Admission{})
	}
	svc := anchoredAlbumsServiceWith(t, funcMBAnchor(func(context.Context, string) ([]string, error) {
		return nil, errors.New("mb unavailable")
	}), searchtest.ArtistBreaker(openBreaker))
	resp, err := svc.GetAlbums(context.Background(), domain.ProviderDeezer, "d1", nil, 50)
	if err != nil {
		t.Fatalf("GetAlbums error = %v", err)
	}
	if len(resp.Items) != 1 || !resp.Partial {
		t.Fatalf("items = %d, partial = %v, want 1 unfiltered item and partial = true", len(resp.Items), resp.Partial)
	}
	recs := anchorFailedRecords(t, buf)
	if len(recs) != 1 || recs[0]["level"] != "WARN" || recs[0]["mbid"] != "mbid-che" {
		t.Fatalf("records = %v, want exactly one WARN carrying mbid-che", recs)
	}
}

func TestGetAlbums_v2_openBreakerAfterCallerLeftIsSilent(t *testing.T) {
	buf := memory.CaptureProductionLogs(t)
	openBreaker := service.NewCircuitBreaker(clock.Now)
	for range artistBreakerTripsAfter {
		openBreaker.RecordFailure(context.Background(), domain.ProviderMusicBrainz, service.Admission{})
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	deezer := &memory.ArtistContentProvider{
		GetAlbumsFn: func(context.Context, domain.ProviderName, string) ([]domain.SearchResult, error) {
			cancel()
			return []domain.SearchResult{memory.V2Album(domain.ProviderDeezer, "d-fl", "Fully Loaded", withDate("2026-04-01"))}, nil
		},
	}
	svc := searchtest.NewArtistContent(t, map[domain.ProviderName]ports.ArtistContentProvider{domain.ProviderDeezer: deezer}, searchtest.ArtistIdentities(&memory.FakeIdentityStore{Mbid: "mbid-che", Xref: map[domain.ProviderKey]string{"deezer": "d1"}}), searchtest.ArtistAnchor(stubMBAnchor{titles: []string{"Fully Loaded"}}), searchtest.ArtistBreaker(openBreaker))
	resp, err := svc.GetAlbums(ctx, domain.ProviderDeezer, "d1", nil, 50)
	if err != nil {
		t.Fatalf("GetAlbums error = %v", err)
	}
	if len(resp.Items) == 0 {
		t.Fatalf("items = 0, want the provider's album so Partial is checked with items present")
	}
	if resp.Partial {
		t.Errorf("Partial = true with items, want false when the caller left")
	}
	if recs := anchorFailedRecords(t, buf); len(recs) != 0 {
		t.Errorf("records = %v, want none after the caller left", recs)
	}
}

func TestGetAlbums_v2_anchorFailureWarnCarriesNoSecretFromTheErrorURL(t *testing.T) {
	const secret = "s3cr3tVALUEfedcba9876543210"
	urls := map[string]string{
		"token query":         "https://musicbrainz.org/ws/2/release-group?artist=x&token=" + secret,
		"access_token query":  "https://musicbrainz.org/ws/2/release-group?access_token=" + secret + "&fmt=json",
		"client_secret query": "https://musicbrainz.org/ws/2/?client_secret=" + secret,
		"password query":      "https://musicbrainz.org/ws/2/?password=" + secret,
	}
	for name, u := range urls {
		t.Run(name, func(t *testing.T) {
			buf := memory.CaptureProductionLogs(t)
			leaky := &url.Error{Op: "Get", URL: u, Err: errors.New("i/o timeout")}
			resp, err := anchoredAlbumsService(t, stubMBAnchor{err: leaky}).GetAlbums(context.Background(), domain.ProviderDeezer, "d1", nil, 50)
			if err != nil {
				t.Fatalf("GetAlbums error = %v", err)
			}
			if !resp.Partial {
				t.Fatalf("Partial = false, want true so the warn path ran")
			}
			if recs := anchorFailedRecords(t, buf); len(recs) != 1 {
				t.Fatalf("records = %v, want one warn", recs)
			}
			if strings.Contains(buf.String(), secret) {
				t.Errorf("log leaked the secret: %s", buf.String())
			}
		})
	}
}

func TestGetAlbums_v2_anchorFailureWarnCarriesNoBearerToken(t *testing.T) {
	const token = "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ4In0.c2lnbmF0dXJl"
	buf := memory.CaptureProductionLogs(t)
	anchorErr := errors.New("mb anchor: 401 for request with Authorization: Bearer " + token)
	resp, err := anchoredAlbumsService(t, stubMBAnchor{err: anchorErr}).GetAlbums(context.Background(), domain.ProviderDeezer, "d1", nil, 50)
	if err != nil {
		t.Fatalf("GetAlbums error = %v", err)
	}
	if !resp.Partial {
		t.Fatalf("Partial = false, want true so the warn path ran")
	}
	if strings.Contains(buf.String(), token) {
		t.Errorf("log leaked the bearer token: %s", buf.String())
	}
}

func albumsByID(t *testing.T, want string, albums ...domain.SearchResult) *memory.ArtistContentProvider {
	t.Helper()
	return &memory.ArtistContentProvider{
		GetAlbumsFn: func(_ context.Context, _ domain.ProviderName, id string) ([]domain.SearchResult, error) {
			if id != want {
				t.Errorf("queried id %q, want %q", id, want)
			}
			return albums, nil
		},
	}
}

func TestGetAlbums_v2_asksEveryProviderTheSearchCardKnows(t *testing.T) {
	svc := searchtest.NewArtistContent(t, map[domain.ProviderName]ports.ArtistContentProvider{
		domain.ProviderDeezer:  albumsByID(t, "d1", memory.V2Album(domain.ProviderDeezer, "d-rib", "REST IN BASS")),
		domain.ProviderSpotify: albumsByID(t, "sp1", memory.V2Album(domain.ProviderSpotify, "s-rib", "REST IN BASS"), memory.V2Album(domain.ProviderSpotify, "s-fl", "Fully Loaded")),
	}, searchtest.ArtistIdentities(&memory.FakeIdentityStore{}))

	resp, err := svc.GetAlbums(t.Context(), domain.ProviderDeezer, "d1", map[domain.ProviderName]string{domain.ProviderSpotify: "sp1"}, 50)
	if err != nil {
		t.Fatal(err)
	}

	if len(resp.Items) != 2 {
		t.Fatalf("items = %+v, want REST IN BASS and Fully Loaded", resp.Items)
	}
	for _, a := range resp.Items {
		if a.Title == "REST IN BASS" && len(a.Sources) != 2 {
			t.Errorf("REST IN BASS sources = %+v, want Deezer and Spotify", a.Sources)
		}
	}
}

func TestGetAlbums_v2_savedIdentityWinsOverTheSearchCard(t *testing.T) {
	store := &memory.FakeIdentityStore{Mbid: "mbid-che", Xref: map[domain.ProviderKey]string{"spotify": "sp-saved"}}
	svc := searchtest.NewArtistContent(t, map[domain.ProviderName]ports.ArtistContentProvider{
		domain.ProviderDeezer:  albumsByID(t, "d1", memory.V2Album(domain.ProviderDeezer, "d-rib", "REST IN BASS")),
		domain.ProviderSpotify: albumsByID(t, "sp-saved", memory.V2Album(domain.ProviderSpotify, "s-rib", "REST IN BASS")),
	}, searchtest.ArtistIdentities(store))

	if _, err := svc.GetAlbums(t.Context(), domain.ProviderDeezer, "d1", map[domain.ProviderName]string{domain.ProviderSpotify: "sp-card"}, 50); err != nil {
		t.Fatal(err)
	}
}

func TestGetAlbums_v2_dropsSoundCloudOnlyReleasesWhenACatalogKnowsTheArtist(t *testing.T) {
	svc := searchtest.NewArtistContent(t, map[domain.ProviderName]ports.ArtistContentProvider{
		domain.ProviderDeezer:     albumsByID(t, "d1", memory.V2Album(domain.ProviderDeezer, "d-nv", "Nevermind")),
		domain.ProviderSoundCloud: albumsByID(t, "sc1", memory.V2Album(domain.ProviderSoundCloud, "sc-nv", "Nevermind"), memory.V2Album(domain.ProviderSoundCloud, "sc-rc", "Rainbow Chaser")),
	}, searchtest.ArtistIdentities(&memory.FakeIdentityStore{}))

	resp, err := svc.GetAlbums(t.Context(), domain.ProviderDeezer, "d1", map[domain.ProviderName]string{domain.ProviderSoundCloud: "sc1"}, 50)
	if err != nil {
		t.Fatal(err)
	}

	if len(resp.Items) != 1 || resp.Items[0].Title != "Nevermind" {
		t.Fatalf("items = %+v, want only Nevermind (Rainbow Chaser is on SoundCloud only)", resp.Items)
	}
}

func TestGetAlbums_v2_keepsSoundCloudReleasesForAnArtistOnlyOnSoundCloud(t *testing.T) {
	calls := 0
	soundcloud := &memory.ArtistContentProvider{
		GetAlbumsFn: func(context.Context, domain.ProviderName, string) ([]domain.SearchResult, error) {
			calls++
			return []domain.SearchResult{memory.V2Album(domain.ProviderSoundCloud, "sc-tape", "First Tape")}, nil
		},
	}
	svc := searchtest.NewArtistContent(t, map[domain.ProviderName]ports.ArtistContentProvider{
		domain.ProviderSoundCloud: soundcloud,
	}, searchtest.ArtistIdentities(&memory.FakeIdentityStore{}))

	resp, err := svc.GetAlbums(t.Context(), domain.ProviderSoundCloud, "sc1", map[domain.ProviderName]string{domain.ProviderYouTube: "yt1"}, 50)
	if err != nil {
		t.Fatal(err)
	}

	if len(resp.Items) != 1 {
		t.Fatalf("items = %+v, want the SoundCloud tape kept", resp.Items)
	}
	if calls != 1 {
		t.Errorf("SoundCloud asked %d times, want once (kept on the first pass, no fallback)", calls)
	}
}

func TestGetTopTracks_v2_dropsSoundCloudOnlyTracksWhenACatalogKnowsTheArtist(t *testing.T) {
	topTracks := func(tracks ...domain.SearchResult) *memory.ArtistContentProvider {
		return &memory.ArtistContentProvider{
			GetTopTracksFn: func(context.Context, domain.ProviderName, string) ([]domain.SearchResult, error) {
				return tracks, nil
			},
		}
	}
	svc := searchtest.NewArtistContent(t, map[domain.ProviderName]ports.ArtistContentProvider{
		domain.ProviderDeezer:     topTracks(memory.TrackFrom(domain.ProviderDeezer, "d-l", "Lithium", "Nirvana")),
		domain.ProviderSoundCloud: topTracks(memory.TrackFrom(domain.ProviderSoundCloud, "sc-r", "Rainbow Chaser", "Nirvana")),
	}, searchtest.ArtistIdentities(&memory.FakeIdentityStore{}))

	resp, err := svc.GetTopTracks(t.Context(), domain.ProviderDeezer, "d1", map[domain.ProviderName]string{domain.ProviderSoundCloud: "sc1"}, 5)
	if err != nil {
		t.Fatal(err)
	}

	if len(resp.Items) != 1 || resp.Items[0].Title != "Lithium" {
		t.Fatalf("items = %+v, want only Lithium", resp.Items)
	}
}

func countingEmptyProvider(albumCalls, trackCalls *int) *memory.ArtistContentProvider {
	return &memory.ArtistContentProvider{
		GetAlbumsFn: func(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
			*albumCalls++
			return nil, nil
		},
		GetTopTracksFn: func(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
			*trackCalls++
			return nil, nil
		},
	}
}

func TestGetArtistContent_v2_anEmptyAnswerIsNotFoundWithoutAskingAgain(t *testing.T) {
	var albumCalls, trackCalls int
	svc := searchtest.NewArtistContent(t, map[domain.ProviderName]ports.ArtistContentProvider{domain.ProviderDeezer: countingEmptyProvider(&albumCalls, &trackCalls)}, searchtest.ArtistIdentities(&memory.FakeIdentityStore{}))

	albums, err := svc.GetAlbums(t.Context(), domain.ProviderDeezer, "d1", nil, 50)
	if err != nil {
		t.Fatal(err)
	}
	tracks, err := svc.GetTopTracks(t.Context(), domain.ProviderDeezer, "d1", nil, 10)
	if err != nil {
		t.Fatal(err)
	}

	if len(albums.Items) != 0 || len(tracks.Items) != 0 {
		t.Errorf("albums = %+v, tracks = %+v, want none", albums.Items, tracks.Items)
	}
	if albumCalls != 1 || trackCalls != 1 {
		t.Errorf("Deezer asked %d times for albums and %d for top tracks, want once each", albumCalls, trackCalls)
	}
}

func TestGetTopTracks_v2_mixedDeezerPageTracksAreFilteredAgainstMB(t *testing.T) {
	mbTitles := make([]string, 0, 30)
	for i := 1; i <= 30; i++ {
		mbTitles = append(mbTitles, "che "+strings.Repeat("i", i))
	}
	mixedPage := func() []domain.SearchResult {
		page := make([]domain.SearchResult, 0, 24)
		for i := 1; i <= 20; i++ {
			page = append(page, memory.V2Album(domain.ProviderDeezer, "d-o"+strings.Repeat("i", i), "other "+strings.Repeat("i", i)))
		}
		for _, title := range mbTitles[:4] {
			page = append(page, memory.V2Album(domain.ProviderDeezer, "d-"+title, title))
		}
		return page
	}
	cleanPage := func() []domain.SearchResult {
		page := make([]domain.SearchResult, 0, 10)
		for _, title := range mbTitles[:10] {
			page = append(page, memory.V2Album(domain.ProviderDeezer, "d-"+title, title))
		}
		return page
	}
	deezerOnly := func() []domain.SearchResult {
		wtf := memory.TrackFrom(domain.ProviderDeezer, "d-wtf", "wtf", "Che")
		wtf.Album = "other i"
		hugo := memory.TrackFrom(domain.ProviderDeezer, "d-hugo", "hugo", "Che")
		hugo.Album = "other ii"
		return []domain.SearchResult{wtf, hugo}
	}
	tests := []struct {
		name   string
		albums []domain.SearchResult
		anchor stubMBAnchor
		want   []string
	}{
		{"mixed page drops its deezer-only tracks", mixedPage(), stubMBAnchor{titles: mbTitles}, []string{}},
		{"clean page keeps every track", cleanPage(), stubMBAnchor{titles: mbTitles}, []string{"wtf", "hugo"}},
		{"MB error returns the tracks unfiltered", mixedPage(), stubMBAnchor{err: errors.New("musicbrainz down")}, []string{"wtf", "hugo"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			deezer := &memory.ArtistContentProvider{
				GetAlbumsFn: func(context.Context, domain.ProviderName, string) ([]domain.SearchResult, error) {
					return tc.albums, nil
				},
				GetTopTracksFn: func(context.Context, domain.ProviderName, string) ([]domain.SearchResult, error) {
					return deezerOnly(), nil
				},
			}
			svc := searchtest.NewArtistContent(t, map[domain.ProviderName]ports.ArtistContentProvider{domain.ProviderDeezer: deezer}, searchtest.ArtistIdentities(&memory.FakeIdentityStore{Mbid: "mbid-che", Xref: map[domain.ProviderKey]string{"deezer": "d1"}}), searchtest.ArtistAnchor(tc.anchor))

			resp, err := svc.GetTopTracks(t.Context(), domain.ProviderDeezer, "d1", nil, 10)
			if err != nil {
				t.Fatalf("GetTopTracks error = %v", err)
			}

			got := make([]string, 0, len(resp.Items))
			for _, r := range resp.Items {
				got = append(got, r.Title)
			}
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Errorf("tracks = %v, want %v", got, tc.want)
			}
		})
	}
}
