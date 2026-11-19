package content_test

import (
	"altune/go-api/internal/discovery"
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
	sampleTracks := []discovery.SearchResult{
		{Kind: discovery.ResultKindTrack, Title: "Blinding Lights", Subtitle: "The Weeknd", Sources: []discovery.SourceRef{{Provider: discovery.ProviderDeezer, ExternalID: "t1"}}},
		{Kind: discovery.ResultKindTrack, Title: "Save Your Tears", Subtitle: "The Weeknd", Sources: []discovery.SourceRef{{Provider: discovery.ProviderDeezer, ExternalID: "t2"}}},
		{Kind: discovery.ResultKindTrack, Title: "Starboy", Subtitle: "The Weeknd", Sources: []discovery.SourceRef{{Provider: discovery.ProviderDeezer, ExternalID: "t3"}}},
	}

	tests := []struct {
		name          string
		providerName  discovery.ProviderName
		externalID    string
		limit         int
		providers     map[discovery.ProviderName]discovery.ArtistContentProvider
		wantStatus    discovery.ProviderStatus
		wantItemCount int
	}{
		{
			name:         "valid provider returns top tracks",
			providerName: discovery.ProviderDeezer,
			externalID:   "artist-42",
			limit:        0,
			providers: map[discovery.ProviderName]discovery.ArtistContentProvider{
				discovery.ProviderDeezer: &discovery.FakeArtistContentProvider{
					GetTopTracksFn: func(_ context.Context, pn discovery.ProviderName, extID string) ([]discovery.SearchResult, error) {
						if pn != discovery.ProviderDeezer {
							t.Errorf("expected provider deezer, got %s", pn.String())
						}
						if extID != "artist-42" {
							t.Errorf("expected externalID artist-42, got %s", extID)
						}
						return sampleTracks, nil
					},
				},
			},
			wantStatus:    discovery.ProviderStatusOK,
			wantItemCount: 3,
		},
		{
			name:          "unknown provider returns error status",
			providerName:  discovery.ProviderSoundCloud,
			externalID:    "artist-42",
			limit:         0,
			providers:     map[discovery.ProviderName]discovery.ArtistContentProvider{},
			wantStatus:    discovery.ProviderStatusError,
			wantItemCount: 0,
		},
		{
			name:         "provider error returns error status",
			providerName: discovery.ProviderDeezer,
			externalID:   "artist-err",
			limit:        0,
			providers: map[discovery.ProviderName]discovery.ArtistContentProvider{
				discovery.ProviderDeezer: &discovery.FakeArtistContentProvider{
					GetTopTracksFn: func(_ context.Context, _ discovery.ProviderName, _ string) ([]discovery.SearchResult, error) {
						return nil, errors.New("network error")
					},
				},
			},
			wantStatus:    discovery.ProviderStatusError,
			wantItemCount: 0,
		},
		{
			name:         "limit truncates results",
			providerName: discovery.ProviderDeezer,
			externalID:   "artist-42",
			limit:        2,
			providers: map[discovery.ProviderName]discovery.ArtistContentProvider{
				discovery.ProviderDeezer: &discovery.FakeArtistContentProvider{
					GetTopTracksFn: func(_ context.Context, _ discovery.ProviderName, _ string) ([]discovery.SearchResult, error) {
						return sampleTracks, nil
					},
				},
			},
			wantStatus:    discovery.ProviderStatusOK,
			wantItemCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := discovery.NewGetArtistContentService(tt.providers)

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
		providerName  discovery.ProviderName
		externalID    string
		limit         int
		providers     map[discovery.ProviderName]discovery.ArtistContentProvider
		wantStatus    discovery.ProviderStatus
		wantItemCount int
	}{
		{
			name:         "valid provider returns albums",
			providerName: discovery.ProviderDeezer,
			externalID:   "artist-42",
			limit:        0,
			providers: map[discovery.ProviderName]discovery.ArtistContentProvider{
				discovery.ProviderDeezer: &discovery.FakeArtistContentProvider{
					GetAlbumsFn: func(_ context.Context, _ discovery.ProviderName, _ string) ([]discovery.SearchResult, error) {
						return []discovery.SearchResult{
							{Kind: discovery.ResultKindAlbum, Title: "After Hours", Sources: []discovery.SourceRef{{Provider: discovery.ProviderDeezer, ExternalID: "a1"}}},
							{Kind: discovery.ResultKindAlbum, Title: "Starboy", Sources: []discovery.SourceRef{{Provider: discovery.ProviderDeezer, ExternalID: "a2"}}},
						}, nil
					},
				},
			},
			wantStatus:    discovery.ProviderStatusOK,
			wantItemCount: 2,
		},
		{
			name:          "unknown provider returns error status",
			providerName:  discovery.ProviderSoundCloud,
			externalID:    "artist-42",
			limit:         0,
			providers:     map[discovery.ProviderName]discovery.ArtistContentProvider{},
			wantStatus:    discovery.ProviderStatusError,
			wantItemCount: 0,
		},
		{
			name:         "dedup by normalized title keeps album with higher track_count",
			providerName: discovery.ProviderDeezer,
			externalID:   "artist-42",
			limit:        0,
			providers: map[discovery.ProviderName]discovery.ArtistContentProvider{
				discovery.ProviderDeezer: &discovery.FakeArtistContentProvider{
					GetAlbumsFn: func(_ context.Context, _ discovery.ProviderName, _ string) ([]discovery.SearchResult, error) {
						return []discovery.SearchResult{
							{
								Kind:       discovery.ResultKindAlbum,
								Title:      "After Hours",
								Sources:    []discovery.SourceRef{{Provider: discovery.ProviderDeezer, ExternalID: "a1"}},
								TrackCount: 14,
							},
							{
								Kind:       discovery.ResultKindAlbum,
								Title:      "After Hours (Deluxe)",
								Sources:    []discovery.SourceRef{{Provider: discovery.ProviderDeezer, ExternalID: "a2"}},
								TrackCount: 18,
							},
							{
								Kind:       discovery.ResultKindAlbum,
								Title:      "Starboy",
								Sources:    []discovery.SourceRef{{Provider: discovery.ProviderDeezer, ExternalID: "a3"}},
								TrackCount: 18,
							},
						}, nil
					},
				},
			},
			wantStatus:    discovery.ProviderStatusOK,
			wantItemCount: 2,
		},
		{
			name:         "dedup keeps higher track_count version",
			providerName: discovery.ProviderDeezer,
			externalID:   "artist-42",
			limit:        0,
			providers: map[discovery.ProviderName]discovery.ArtistContentProvider{
				discovery.ProviderDeezer: &discovery.FakeArtistContentProvider{
					GetAlbumsFn: func(_ context.Context, _ discovery.ProviderName, _ string) ([]discovery.SearchResult, error) {
						return []discovery.SearchResult{
							{
								Kind:       discovery.ResultKindAlbum,
								Title:      "Dawn FM",
								Sources:    []discovery.SourceRef{{Provider: discovery.ProviderDeezer, ExternalID: "a1"}},
								TrackCount: 10,
							},
							{
								Kind:       discovery.ResultKindAlbum,
								Title:      "Dawn FM (Alternate World)",
								Sources:    []discovery.SourceRef{{Provider: discovery.ProviderDeezer, ExternalID: "a2"}},
								TrackCount: 20,
							},
						}, nil
					},
				},
			},
			wantStatus:    discovery.ProviderStatusOK,
			wantItemCount: 1,
		},
		{
			name:         "provider error returns error status",
			providerName: discovery.ProviderDeezer,
			externalID:   "artist-err",
			limit:        0,
			providers: map[discovery.ProviderName]discovery.ArtistContentProvider{
				discovery.ProviderDeezer: &discovery.FakeArtistContentProvider{
					GetAlbumsFn: func(_ context.Context, _ discovery.ProviderName, _ string) ([]discovery.SearchResult, error) {
						return nil, errors.New("api failure")
					},
				},
			},
			wantStatus:    discovery.ProviderStatusError,
			wantItemCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := discovery.NewGetArtistContentService(tt.providers)

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
	album := func(title, releaseDate, extID string) discovery.SearchResult {
		return discovery.SearchResult{
			Kind:        discovery.ResultKindAlbum,
			Title:       title,
			Sources:     []discovery.SourceRef{{Provider: discovery.ProviderDeezer, ExternalID: extID}},
			ReleaseDate: releaseDate,
		}
	}

	t.Run("sorts newest-first and normalizes year from release_date", func(t *testing.T) {
		providers := map[discovery.ProviderName]discovery.ArtistContentProvider{
			discovery.ProviderDeezer: &discovery.FakeArtistContentProvider{
				GetAlbumsFn: func(_ context.Context, _ discovery.ProviderName, _ string) ([]discovery.SearchResult, error) {
					return []discovery.SearchResult{
						album("Older", "2016-09-09", "a1"),
						album("Newest", "2022-01-07", "a3"),
						album("Middle", "2020-03-20", "a2"),
					}, nil
				},
			},
		}
		svc := discovery.NewGetArtistContentService(providers)

		resp, err := svc.GetAlbums(context.Background(), discovery.ProviderDeezer, "artist-1", nil, 0)
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
		providers := map[discovery.ProviderName]discovery.ArtistContentProvider{
			discovery.ProviderDeezer: &discovery.FakeArtistContentProvider{
				GetAlbumsFn: func(_ context.Context, _ discovery.ProviderName, _ string) ([]discovery.SearchResult, error) {
					return []discovery.SearchResult{
						album("NoDate", "", "a1"),
						album("Dated", "2019-05-01", "a2"),
					}, nil
				},
			},
		}
		svc := discovery.NewGetArtistContentService(providers)

		resp, err := svc.GetAlbums(context.Background(), discovery.ProviderDeezer, "artist-1", nil, 0)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp.Items) != 2 || resp.Items[0].Title != "Dated" || resp.Items[1].Title != "NoDate" {
			t.Errorf("expected [Dated, NoDate], got %v", discovery.AlbumTitles(resp.Items))
		}
	})

	t.Run("limit keeps the newest after sorting", func(t *testing.T) {
		providers := map[discovery.ProviderName]discovery.ArtistContentProvider{
			discovery.ProviderDeezer: &discovery.FakeArtistContentProvider{
				GetAlbumsFn: func(_ context.Context, _ discovery.ProviderName, _ string) ([]discovery.SearchResult, error) {
					return []discovery.SearchResult{
						album("Old", "2010-01-01", "a1"),
						album("New", "2024-01-01", "a2"),
						album("Mid", "2017-01-01", "a3"),
					}, nil
				},
			},
		}
		svc := discovery.NewGetArtistContentService(providers)

		resp, err := svc.GetAlbums(context.Background(), discovery.ProviderDeezer, "artist-1", nil, 2)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp.Items) != 2 || resp.Items[0].Title != "New" || resp.Items[1].Title != "Mid" {
			t.Errorf("expected [New, Mid], got %v", discovery.AlbumTitles(resp.Items))
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
			svc := identityFanOutWithErr(func(pn discovery.ProviderName) error {
				if pn == discovery.ProviderTheAudioDB || pn == discovery.ProviderSpotify {
					return leaky
				}
				return nil
			})
			buf := discovery.CaptureProductionLogs(t)

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
			for _, pn := range []discovery.ProviderName{discovery.ProviderTheAudioDB, discovery.ProviderSpotify} {
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
	svc := identityFanOutWithErr(func(discovery.ProviderName) error { return nil })
	buf := discovery.CaptureProductionLogs(t)

	if _, err := svc.GetTopTracks(context.Background(), discovery.ProviderDeezer, "id-deezer", nil, 10); err != nil {
		t.Fatalf("error = %v", err)
	}
	if recs := fanOutFailureRecords(t, buf); len(recs) != 0 {
		t.Errorf("got %d failure records, want none:\n%s", len(recs), buf)
	}
}

func TestIdentityFanOut_NoFailureWarnWhenCallerCancels(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc := identityFanOutWithErr(func(pn discovery.ProviderName) error {
		if pn == discovery.ProviderDeezer {
			return nil
		}
		cancel()
		return context.Canceled
	})
	buf := discovery.CaptureProductionLogs(t)

	if _, err := svc.GetTopTracks(ctx, discovery.ProviderDeezer, "id-deezer", nil, 10); err != nil {
		t.Fatalf("error = %v", err)
	}
	if recs := fanOutFailureRecords(t, buf); len(recs) != 0 {
		t.Errorf("got %d failure records for a cancelled request, want none:\n%s", len(recs), buf)
	}
}

func TestIdentityFanOut_NoFailureWarnForCircuitOpenSkip(t *testing.T) {
	cb := discovery.NewCircuitBreaker()
	tripCircuit(t, cb, discovery.ProviderSpotify)
	svc := identityFanOutWithErr(func(discovery.ProviderName) error { return nil }, discovery.WithContentCircuitBreaker(cb))
	buf := discovery.CaptureProductionLogs(t)

	if _, err := svc.GetTopTracks(context.Background(), discovery.ProviderDeezer, "id-deezer", nil, 10); err != nil {
		t.Fatalf("error = %v", err)
	}
	if recs := fanOutFailureRecords(t, buf); len(recs) != 0 {
		t.Errorf("got %d failure records for a circuit-open skip, want none:\n%s", len(recs), buf)
	}
}

func identityFanOutWithErr(errFor func(discovery.ProviderName) error, opts ...discovery.ArtistContentOption) *discovery.GetArtistContentService {
	providers := make(map[discovery.ProviderName]discovery.ArtistContentProvider, len(everyContentProvider))
	xref := make(map[discovery.ProviderKey]string, len(everyContentProvider))
	for _, name := range everyContentProvider {
		xref[name.Key()] = "id-" + name.String()
		providers[name] = &discovery.FakeArtistContentProvider{
			GetTopTracksFn: func(_ context.Context, pn discovery.ProviderName, id string) ([]discovery.SearchResult, error) {
				if err := errFor(pn); err != nil {
					return nil, err
				}
				return []discovery.SearchResult{discovery.TrackFrom(pn, id, "Real Song", "Che")}, nil
			},
			GetAlbumsFn: func(_ context.Context, pn discovery.ProviderName, id string) ([]discovery.SearchResult, error) {
				if err := errFor(pn); err != nil {
					return nil, err
				}
				return []discovery.SearchResult{discovery.V2Album(pn, id, "Fully Loaded", withDate("2026-04-01"))}, nil
			},
		}
	}
	store := &discovery.FakeIdentityStore{Mbid: "mbid-che", Xref: xref}
	return discovery.NewGetArtistContentService(providers, append(opts, discovery.WithContentIdentityStore(store))...)
}

var everyContentProvider = []discovery.ProviderName{
	discovery.ProviderDeezer, discovery.ProviderMusicBrainz, discovery.ProviderSoundCloud,
	discovery.ProviderITunes, discovery.ProviderTheAudioDB,
	discovery.ProviderDiscogs, discovery.ProviderYouTube,
	discovery.ProviderAppleMusic, discovery.ProviderSpotify,
}

func identityFanOut(fail func(discovery.ProviderName) bool, opts ...discovery.ArtistContentOption) *discovery.GetArtistContentService {
	return identityFanOutWithErr(func(pn discovery.ProviderName) error {
		if fail(pn) {
			return discovery.UpstreamDown
		}
		return nil
	}, opts...)
}

type contentFetcher func(*discovery.GetArtistContentService) (*discovery.ContentFetchResponse, error)

var identityContentFetchers = map[string]contentFetcher{
	"top tracks": func(s *discovery.GetArtistContentService) (*discovery.ContentFetchResponse, error) {
		return s.GetTopTracks(context.Background(), discovery.ProviderDeezer, "id-deezer", nil, 10)
	},
	"albums": func(s *discovery.GetArtistContentService) (*discovery.ContentFetchResponse, error) {
		return s.GetAlbums(context.Background(), discovery.ProviderDeezer, "id-deezer", nil, 50)
	},
}

func TestIdentityFanOut_MostProvidersFailedReportsPartial(t *testing.T) {
	for name, fetch := range identityContentFetchers {
		t.Run(name, func(t *testing.T) {
			svc := identityFanOut(func(pn discovery.ProviderName) bool { return pn != discovery.ProviderDeezer })

			resp, err := fetch(svc)
			if err != nil {
				t.Fatalf("error = %v", err)
			}
			if resp.Status != discovery.ProviderStatusOK || len(resp.Items) != 1 {
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
			resp, err := fetch(identityFanOut(func(discovery.ProviderName) bool { return false }))
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
	cb := discovery.NewCircuitBreaker()
	tripCircuit(t, cb, discovery.ProviderSpotify)
	svc := identityFanOut(func(discovery.ProviderName) bool { return false }, discovery.WithContentCircuitBreaker(cb))

	resp, err := svc.GetTopTracks(context.Background(), discovery.ProviderDeezer, "id-deezer", nil, 10)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if !resp.Partial {
		t.Error("partial = false, want true (spotify's circuit is open)")
	}
}

func TestIdentityFanOut_ProviderWithoutIDDoesNotMakePartial(t *testing.T) {
	answer := &discovery.FakeArtistContentProvider{
		GetTopTracksFn: func(_ context.Context, pn discovery.ProviderName, id string) ([]discovery.SearchResult, error) {
			return []discovery.SearchResult{discovery.TrackFrom(pn, id, "Real Song", "Che")}, nil
		},
	}
	unreachable := &discovery.FakeArtistContentProvider{
		GetTopTracksFn: func(context.Context, discovery.ProviderName, string) ([]discovery.SearchResult, error) {
			t.Error("provider without a stored ID was called")
			return nil, discovery.UpstreamDown
		},
	}
	cb := discovery.NewCircuitBreaker()
	tripCircuit(t, cb, discovery.ProviderYouTube)
	svc := discovery.NewGetArtistContentService(
		map[discovery.ProviderName]discovery.ArtistContentProvider{
			discovery.ProviderDeezer:  answer,
			discovery.ProviderDiscogs: unreachable,
			discovery.ProviderYouTube: unreachable,
		},
		discovery.WithContentIdentityStore(&discovery.FakeIdentityStore{Mbid: "mbid-che", Xref: map[discovery.ProviderKey]string{"deezer": "d1"}}),
		discovery.WithContentCircuitBreaker(cb),
	)

	resp, err := svc.GetTopTracks(context.Background(), discovery.ProviderDeezer, "d1", nil, 10)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(resp.Items) != 1 || resp.Partial {
		t.Errorf("items = %d, partial = %v, want 1 item and partial = false", len(resp.Items), resp.Partial)
	}
}

func TestGetTopTracks_PanickingProviderIsContained(t *testing.T) {
	svc := discovery.NewGetArtistContentService(map[discovery.ProviderName]discovery.ArtistContentProvider{
		discovery.ProviderDeezer: &discovery.FakeArtistContentProvider{
			GetTopTracksFn: func(context.Context, discovery.ProviderName, string) ([]discovery.SearchResult, error) {
				panic("deezer exploded")
			},
		},
		discovery.ProviderITunes: &discovery.FakeArtistContentProvider{
			GetTopTracksFn: func(_ context.Context, provider discovery.ProviderName, id string) ([]discovery.SearchResult, error) {
				return []discovery.SearchResult{discovery.TrackFrom(provider, id, "Track", "Artist")}, nil
			},
		},
	})
	known := map[discovery.ProviderName]string{discovery.ProviderDeezer: "dz", discovery.ProviderITunes: "it"}

	resp, err := svc.GetTopTracks(context.Background(), discovery.ProviderDeezer, "dz", known, 10)
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
	truncated := &discovery.PartialResultError{Page: 2, Err: discovery.UpstreamDown}
	for name, fetch := range identityContentFetchers {
		t.Run("identity fan-out "+name, func(t *testing.T) {
			svc := identityFanOutWithErr(func(pn discovery.ProviderName) error {
				if pn == discovery.ProviderDeezer {
					return truncated
				}
				return nil
			})
			resp, err := fetch(svc)
			if err != nil {
				t.Fatalf("error = %v", err)
			}
			if resp.Status != discovery.ProviderStatusOK || len(resp.Items) == 0 {
				t.Fatalf("status = %s, items = %d, want ok with the merged items", resp.Status, len(resp.Items))
			}
			if !resp.Partial {
				t.Error("partial = false, want true: a provider's later page failed")
			}
		})
	}

	t.Run("single provider albums", func(t *testing.T) {
		svc := discovery.NewGetArtistContentService(map[discovery.ProviderName]discovery.ArtistContentProvider{
			discovery.ProviderDeezer: &discovery.FakeArtistContentProvider{
				GetAlbumsFn: func(_ context.Context, pn discovery.ProviderName, id string) ([]discovery.SearchResult, error) {
					return []discovery.SearchResult{discovery.V2Album(pn, id, "Fully Loaded", withDate("2026-04-01"))}, truncated
				},
			},
		})
		resp, err := svc.GetAlbums(context.Background(), discovery.ProviderDeezer, "id-deezer", nil, 50)
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if resp.Status != discovery.ProviderStatusOK || len(resp.Items) != 1 || !resp.Partial {
			t.Errorf("status = %s, items = %d, partial = %v, want ok, the page-0 item, partial", resp.Status, len(resp.Items), resp.Partial)
		}
	})
}

const breakerTripsAfter = 5

func tripCircuit(t *testing.T, cb *discovery.CircuitBreaker, provider discovery.ProviderName) {
	t.Helper()
	for range breakerTripsAfter {
		cb.RecordFailure(context.Background(), provider)
	}
	if cb.GetStatus(provider) != discovery.ProviderStatusCircuitOpen {
		t.Fatalf("setup: %s circuit not open after %d failures", provider, breakerTripsAfter)
	}
}

func withDate(d string) func(*discovery.SearchResult) {
	return func(r *discovery.SearchResult) { r.ReleaseDate = d }
}

func withTracks(n int) func(*discovery.SearchResult) {
	return func(r *discovery.SearchResult) { r.TrackCount = n }
}

func withCover(u string) func(*discovery.SearchResult) {
	return func(r *discovery.SearchResult) { r.ImageURL = u }
}

func withType(t discovery.RecordType) func(*discovery.SearchResult) {
	return func(r *discovery.SearchResult) { r.RecordType = t }
}

func TestGetAlbums_v2_bestOfMerge(t *testing.T) {
	deezer := &discovery.FakeArtistContentProvider{
		GetAlbumsFn: func(_ context.Context, _ discovery.ProviderName, _ string) ([]discovery.SearchResult, error) {
			return []discovery.SearchResult{
				discovery.V2Album(discovery.ProviderDeezer, "d-fl", "Fully Loaded",
					withDate("2026-04-01"), withCover("cover-dz"), withType("ep")),
			}, nil
		},
	}
	itunes := &discovery.FakeArtistContentProvider{
		GetAlbumsFn: func(_ context.Context, _ discovery.ProviderName, _ string) ([]discovery.SearchResult, error) {
			return []discovery.SearchResult{
				discovery.V2Album(discovery.ProviderITunes, "i-fl", "Fully Loaded", withTracks(5)),
			}, nil
		},
	}
	store := &discovery.FakeIdentityStore{Mbid: "mbid-che", Xref: map[discovery.ProviderKey]string{"deezer": "d1", "itunes": "i1"}}
	svc := discovery.NewGetArtistContentService(
		map[discovery.ProviderName]discovery.ArtistContentProvider{
			discovery.ProviderDeezer: deezer,
			discovery.ProviderITunes: itunes,
		},
		discovery.WithContentIdentityStore(store),
	)

	resp, err := svc.GetAlbums(context.Background(), discovery.ProviderDeezer, "d1", nil, 50)
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
	deezer := &discovery.FakeArtistContentProvider{
		GetAlbumsFn: func(_ context.Context, _ discovery.ProviderName, id string) ([]discovery.SearchResult, error) {
			if id != "399574001" {
				t.Errorf("deezer queried id %q, want the seed 399574001", id)
			}
			return []discovery.SearchResult{
				discovery.V2Album(discovery.ProviderDeezer, "d-rib", "REST IN BASS: ENCORE", withDate("2025-12-25"), withCover("c")),
			}, nil
		},
	}
	svc := discovery.NewGetArtistContentService(
		map[discovery.ProviderName]discovery.ArtistContentProvider{discovery.ProviderDeezer: deezer},
		discovery.WithContentIdentityStore(&discovery.FakeIdentityStore{}),
	)

	resp, err := svc.GetAlbums(context.Background(), discovery.ProviderDeezer, "399574001", nil, 50)
	if err != nil {
		t.Fatalf("GetAlbums error = %v", err)
	}
	if len(resp.Items) != 1 || resp.Items[0].Title != "REST IN BASS: ENCORE" {
		t.Fatalf("items = %+v, want just the id-verified ENCORE (V2 ran on seed)", resp.Items)
	}
}

func TestGetAlbums_v2_deterministicAcrossRuns(t *testing.T) {
	deezer := &discovery.FakeArtistContentProvider{
		GetAlbumsFn: func(_ context.Context, _ discovery.ProviderName, _ string) ([]discovery.SearchResult, error) {
			return []discovery.SearchResult{
				discovery.V2Album(discovery.ProviderDeezer, "d-fl", "Fully Loaded", withDate("2026-04-01")),
			}, nil
		},
	}
	itunes := &discovery.FakeArtistContentProvider{
		GetAlbumsFn: func(_ context.Context, _ discovery.ProviderName, _ string) ([]discovery.SearchResult, error) {
			return []discovery.SearchResult{
				discovery.V2Album(discovery.ProviderITunes, "i-fl", "FULLY LOADED", withTracks(5)),
			}, nil
		},
	}
	store := &discovery.FakeIdentityStore{Mbid: "mbid-che", Xref: map[discovery.ProviderKey]string{"deezer": "d1", "itunes": "i1"}}
	svc := discovery.NewGetArtistContentService(
		map[discovery.ProviderName]discovery.ArtistContentProvider{
			discovery.ProviderDeezer: deezer,
			discovery.ProviderITunes: itunes,
		},
		discovery.WithContentIdentityStore(store),
	)

	for i := 0; i < 25; i++ {
		resp, err := svc.GetAlbums(context.Background(), discovery.ProviderDeezer, "d1", nil, 50)
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

func failOnceThenEmptyProvider() *discovery.FakeArtistContentProvider {
	var albumCalls, trackCalls int
	return &discovery.FakeArtistContentProvider{
		GetAlbumsFn: func(_ context.Context, _ discovery.ProviderName, _ string) ([]discovery.SearchResult, error) {
			albumCalls++
			if albumCalls == 1 {
				return nil, errors.New("provider down")
			}
			return nil, nil
		},
		GetTopTracksFn: func(_ context.Context, _ discovery.ProviderName, _ string) ([]discovery.SearchResult, error) {
			trackCalls++
			if trackCalls == 1 {
				return nil, errors.New("provider down")
			}
			return nil, nil
		},
	}
}

func fallbackPartialService(p *discovery.FakeArtistContentProvider) *discovery.GetArtistContentService {
	store := &discovery.FakeIdentityStore{Mbid: "mbid-che", Xref: map[discovery.ProviderKey]string{"deezer": "d1"}}
	return discovery.NewGetArtistContentService(
		map[discovery.ProviderName]discovery.ArtistContentProvider{discovery.ProviderDeezer: p},
		discovery.WithContentIdentityStore(store),
	)
}

func TestGetAlbums_v2_fallbackKeepsPartialWhenIdentityFanOutFailed(t *testing.T) {
	resp, err := fallbackPartialService(failOnceThenEmptyProvider()).GetAlbums(context.Background(), discovery.ProviderDeezer, "d1", nil, 50)
	if err != nil {
		t.Fatalf("GetAlbums error = %v", err)
	}
	if !resp.Partial {
		t.Fatalf("Partial = false, want true after v2 failed with zero results")
	}
}

func TestGetTopTracks_v2_fallbackKeepsPartialWhenIdentityFanOutFailed(t *testing.T) {
	resp, err := fallbackPartialService(failOnceThenEmptyProvider()).GetTopTracks(context.Background(), discovery.ProviderDeezer, "d1", nil, 10)
	if err != nil {
		t.Fatalf("GetTopTracks error = %v", err)
	}
	if !resp.Partial {
		t.Fatalf("Partial = false, want true after v2 failed with zero results")
	}
}

func TestGetArtistContent_v2_fallbackNotPartialWhenNoProviderFailed(t *testing.T) {
	svc := fallbackPartialService(&discovery.FakeArtistContentProvider{
		GetAlbumsFn: func(_ context.Context, _ discovery.ProviderName, _ string) ([]discovery.SearchResult, error) {
			return nil, nil
		},
		GetTopTracksFn: func(_ context.Context, _ discovery.ProviderName, _ string) ([]discovery.SearchResult, error) {
			return nil, nil
		},
	})
	albums, err := svc.GetAlbums(context.Background(), discovery.ProviderDeezer, "d1", nil, 50)
	if err != nil {
		t.Fatalf("GetAlbums error = %v", err)
	}
	if albums.Partial {
		t.Errorf("GetAlbums Partial = true, want false when no provider failed")
	}
	tracks, err := svc.GetTopTracks(context.Background(), discovery.ProviderDeezer, "d1", nil, 10)
	if err != nil {
		t.Fatalf("GetTopTracks error = %v", err)
	}
	if tracks.Partial {
		t.Errorf("GetTopTracks Partial = true, want false when no provider failed")
	}
}

func TestGetTopTracks_v2_corroboratedFirst(t *testing.T) {
	deezer := &discovery.FakeArtistContentProvider{
		GetTopTracksFn: func(_ context.Context, _ discovery.ProviderName, _ string) ([]discovery.SearchResult, error) {
			return []discovery.SearchResult{
				discovery.TrackFrom(discovery.ProviderDeezer, "d-real", "Real Song", "Che"),
				discovery.TrackFrom(discovery.ProviderDeezer, "d-solo", "Solo", "Che"),
			}, nil
		},
	}
	itunes := &discovery.FakeArtistContentProvider{
		GetTopTracksFn: func(_ context.Context, _ discovery.ProviderName, _ string) ([]discovery.SearchResult, error) {
			return []discovery.SearchResult{discovery.TrackFrom(discovery.ProviderITunes, "i-real", "Real Song", "Che")}, nil
		},
	}
	store := &discovery.FakeIdentityStore{Mbid: "mbid-che", Xref: map[discovery.ProviderKey]string{"deezer": "d1", "itunes": "i1"}}
	svc := discovery.NewGetArtistContentService(
		map[discovery.ProviderName]discovery.ArtistContentProvider{
			discovery.ProviderDeezer: deezer,
			discovery.ProviderITunes: itunes,
		},
		discovery.WithContentIdentityStore(store),
	)

	resp, err := svc.GetTopTracks(context.Background(), discovery.ProviderDeezer, "d1", nil, 10)
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

type stubMBAnchor struct {
	titles []string
	err    error
}

func (a stubMBAnchor) ReleaseGroupTitles(context.Context, string) ([]string, error) {
	return a.titles, a.err
}

func anchoredAlbumsService(anchor stubMBAnchor, extra ...discovery.ArtistContentOption) *discovery.GetArtistContentService {
	deezer := &discovery.FakeArtistContentProvider{
		GetAlbumsFn: func(context.Context, discovery.ProviderName, string) ([]discovery.SearchResult, error) {
			return []discovery.SearchResult{discovery.V2Album(discovery.ProviderDeezer, "d-fl", "Fully Loaded", withDate("2026-04-01"))}, nil
		},
	}
	opts := append([]discovery.ArtistContentOption{
		discovery.WithContentIdentityStore(&discovery.FakeIdentityStore{Mbid: "mbid-che", Xref: map[discovery.ProviderKey]string{"deezer": "d1"}}),
		discovery.WithMBAnchor(anchor),
	}, extra...)
	return discovery.NewGetArtistContentService(map[discovery.ProviderName]discovery.ArtistContentProvider{discovery.ProviderDeezer: deezer}, opts...)
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
	openBreaker := discovery.NewCircuitBreaker()
	for range breakerTripsAfter {
		openBreaker.RecordFailure(context.Background(), discovery.ProviderMusicBrainz)
	}
	cases := map[string]*discovery.GetArtistContentService{
		"anchor error": anchoredAlbumsService(stubMBAnchor{err: leaky}),
		"breaker open": anchoredAlbumsService(stubMBAnchor{titles: []string{"Fully Loaded"}}, discovery.WithContentCircuitBreaker(openBreaker)),
	}
	for name, svc := range cases {
		t.Run(name, func(t *testing.T) {
			buf := discovery.CaptureProductionLogs(t)
			resp, err := svc.GetAlbums(context.Background(), discovery.ProviderDeezer, "d1", nil, 50)
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
	buf := discovery.CaptureProductionLogs(t)
	resp, err := anchoredAlbumsService(stubMBAnchor{}).GetAlbums(context.Background(), discovery.ProviderDeezer, "d1", nil, 50)
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
	buf := discovery.CaptureProductionLogs(t)
	ctx, cancel := context.WithCancel(context.Background())
	svc := anchoredAlbumsService(stubMBAnchor{err: context.Canceled})
	cancel()
	resp, err := svc.GetAlbums(ctx, discovery.ProviderDeezer, "d1", nil, 50)
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

func anchoredAlbumsServiceWith(anchor discovery.MBDiscographyAnchor, extra ...discovery.ArtistContentOption) *discovery.GetArtistContentService {
	deezer := &discovery.FakeArtistContentProvider{
		GetAlbumsFn: func(context.Context, discovery.ProviderName, string) ([]discovery.SearchResult, error) {
			return []discovery.SearchResult{discovery.V2Album(discovery.ProviderDeezer, "d-fl", "Fully Loaded", withDate("2026-04-01"))}, nil
		},
	}
	opts := append([]discovery.ArtistContentOption{
		discovery.WithContentIdentityStore(&discovery.FakeIdentityStore{Mbid: "mbid-che", Xref: map[discovery.ProviderKey]string{"deezer": "d1"}}),
		discovery.WithMBAnchor(anchor),
	}, extra...)
	return discovery.NewGetArtistContentService(map[discovery.ProviderName]discovery.ArtistContentProvider{discovery.ProviderDeezer: deezer}, opts...)
}

func TestGetAlbums_v2_callerLeavingDuringAnchorLookupIsSilentAndNotPartial(t *testing.T) {
	buf := discovery.CaptureProductionLogs(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	anchorCalled := false
	svc := anchoredAlbumsServiceWith(funcMBAnchor(func(context.Context, string) ([]string, error) {
		anchorCalled = true
		cancel()
		return nil, context.Canceled
	}))
	resp, err := svc.GetAlbums(ctx, discovery.ProviderDeezer, "d1", nil, 50)
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
	buf := discovery.CaptureProductionLogs(t)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	anchorCalled := false
	svc := anchoredAlbumsServiceWith(funcMBAnchor(func(context.Context, string) ([]string, error) {
		anchorCalled = true
		<-ctx.Done()
		return nil, ctx.Err()
	}))
	resp, err := svc.GetAlbums(ctx, discovery.ProviderDeezer, "d1", nil, 50)
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
			buf := discovery.CaptureProductionLogs(t)
			svc := anchoredAlbumsServiceWith(funcMBAnchor(func(context.Context, string) ([]string, error) {
				return nil, anchorErr
			}))
			resp, err := svc.GetAlbums(context.Background(), discovery.ProviderDeezer, "d1", nil, 50)
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
	buf := discovery.CaptureProductionLogs(t)
	openBreaker := discovery.NewCircuitBreaker()
	for range breakerTripsAfter {
		openBreaker.RecordFailure(context.Background(), discovery.ProviderMusicBrainz)
	}
	svc := anchoredAlbumsServiceWith(funcMBAnchor(func(context.Context, string) ([]string, error) {
		return nil, errors.New("mb unavailable")
	}), discovery.WithContentCircuitBreaker(openBreaker))
	resp, err := svc.GetAlbums(context.Background(), discovery.ProviderDeezer, "d1", nil, 50)
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
	buf := discovery.CaptureProductionLogs(t)
	openBreaker := discovery.NewCircuitBreaker()
	for range breakerTripsAfter {
		openBreaker.RecordFailure(context.Background(), discovery.ProviderMusicBrainz)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	deezer := &discovery.FakeArtistContentProvider{
		GetAlbumsFn: func(context.Context, discovery.ProviderName, string) ([]discovery.SearchResult, error) {
			cancel()
			return []discovery.SearchResult{discovery.V2Album(discovery.ProviderDeezer, "d-fl", "Fully Loaded", withDate("2026-04-01"))}, nil
		},
	}
	svc := discovery.NewGetArtistContentService(
		map[discovery.ProviderName]discovery.ArtistContentProvider{discovery.ProviderDeezer: deezer},
		discovery.WithContentIdentityStore(&discovery.FakeIdentityStore{Mbid: "mbid-che", Xref: map[discovery.ProviderKey]string{"deezer": "d1"}}),
		discovery.WithMBAnchor(stubMBAnchor{titles: []string{"Fully Loaded"}}),
		discovery.WithContentCircuitBreaker(openBreaker),
	)
	resp, err := svc.GetAlbums(ctx, discovery.ProviderDeezer, "d1", nil, 50)
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
			buf := discovery.CaptureProductionLogs(t)
			leaky := &url.Error{Op: "Get", URL: u, Err: errors.New("i/o timeout")}
			resp, err := anchoredAlbumsService(stubMBAnchor{err: leaky}).GetAlbums(context.Background(), discovery.ProviderDeezer, "d1", nil, 50)
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
	buf := discovery.CaptureProductionLogs(t)
	anchorErr := errors.New("mb anchor: 401 for request with Authorization: Bearer " + token)
	resp, err := anchoredAlbumsService(stubMBAnchor{err: anchorErr}).GetAlbums(context.Background(), discovery.ProviderDeezer, "d1", nil, 50)
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

func albumsByID(t *testing.T, want string, albums ...discovery.SearchResult) *discovery.FakeArtistContentProvider {
	t.Helper()
	return &discovery.FakeArtistContentProvider{
		GetAlbumsFn: func(_ context.Context, _ discovery.ProviderName, id string) ([]discovery.SearchResult, error) {
			if id != want {
				t.Errorf("queried id %q, want %q", id, want)
			}
			return albums, nil
		},
	}
}

func TestGetAlbums_v2_asksEveryProviderTheSearchCardKnows(t *testing.T) {
	svc := discovery.NewGetArtistContentService(
		map[discovery.ProviderName]discovery.ArtistContentProvider{
			discovery.ProviderDeezer:  albumsByID(t, "d1", discovery.V2Album(discovery.ProviderDeezer, "d-rib", "REST IN BASS")),
			discovery.ProviderSpotify: albumsByID(t, "sp1", discovery.V2Album(discovery.ProviderSpotify, "s-rib", "REST IN BASS"), discovery.V2Album(discovery.ProviderSpotify, "s-fl", "Fully Loaded")),
		},
		discovery.WithContentIdentityStore(&discovery.FakeIdentityStore{}),
	)

	resp, err := svc.GetAlbums(t.Context(), discovery.ProviderDeezer, "d1", map[discovery.ProviderName]string{discovery.ProviderSpotify: "sp1"}, 50)
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
	store := &discovery.FakeIdentityStore{Mbid: "mbid-che", Xref: map[discovery.ProviderKey]string{"spotify": "sp-saved"}}
	svc := discovery.NewGetArtistContentService(
		map[discovery.ProviderName]discovery.ArtistContentProvider{
			discovery.ProviderDeezer:  albumsByID(t, "d1", discovery.V2Album(discovery.ProviderDeezer, "d-rib", "REST IN BASS")),
			discovery.ProviderSpotify: albumsByID(t, "sp-saved", discovery.V2Album(discovery.ProviderSpotify, "s-rib", "REST IN BASS")),
		},
		discovery.WithContentIdentityStore(store),
	)

	if _, err := svc.GetAlbums(t.Context(), discovery.ProviderDeezer, "d1", map[discovery.ProviderName]string{discovery.ProviderSpotify: "sp-card"}, 50); err != nil {
		t.Fatal(err)
	}
}

func TestGetAlbums_v2_dropsSoundCloudOnlyReleasesWhenACatalogKnowsTheArtist(t *testing.T) {
	svc := discovery.NewGetArtistContentService(
		map[discovery.ProviderName]discovery.ArtistContentProvider{
			discovery.ProviderDeezer:     albumsByID(t, "d1", discovery.V2Album(discovery.ProviderDeezer, "d-nv", "Nevermind")),
			discovery.ProviderSoundCloud: albumsByID(t, "sc1", discovery.V2Album(discovery.ProviderSoundCloud, "sc-nv", "Nevermind"), discovery.V2Album(discovery.ProviderSoundCloud, "sc-rc", "Rainbow Chaser")),
		},
		discovery.WithContentIdentityStore(&discovery.FakeIdentityStore{}),
	)

	resp, err := svc.GetAlbums(t.Context(), discovery.ProviderDeezer, "d1", map[discovery.ProviderName]string{discovery.ProviderSoundCloud: "sc1"}, 50)
	if err != nil {
		t.Fatal(err)
	}

	if len(resp.Items) != 1 || resp.Items[0].Title != "Nevermind" {
		t.Fatalf("items = %+v, want only Nevermind (Rainbow Chaser is on SoundCloud only)", resp.Items)
	}
}

func TestGetAlbums_v2_keepsSoundCloudReleasesForAnArtistOnlyOnSoundCloud(t *testing.T) {
	calls := 0
	soundcloud := &discovery.FakeArtistContentProvider{
		GetAlbumsFn: func(context.Context, discovery.ProviderName, string) ([]discovery.SearchResult, error) {
			calls++
			return []discovery.SearchResult{discovery.V2Album(discovery.ProviderSoundCloud, "sc-tape", "First Tape")}, nil
		},
	}
	svc := discovery.NewGetArtistContentService(
		map[discovery.ProviderName]discovery.ArtistContentProvider{
			discovery.ProviderSoundCloud: soundcloud,
		},
		discovery.WithContentIdentityStore(&discovery.FakeIdentityStore{}),
	)

	resp, err := svc.GetAlbums(t.Context(), discovery.ProviderSoundCloud, "sc1", map[discovery.ProviderName]string{discovery.ProviderYouTube: "yt1"}, 50)
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
	topTracks := func(tracks ...discovery.SearchResult) *discovery.FakeArtistContentProvider {
		return &discovery.FakeArtistContentProvider{
			GetTopTracksFn: func(context.Context, discovery.ProviderName, string) ([]discovery.SearchResult, error) {
				return tracks, nil
			},
		}
	}
	svc := discovery.NewGetArtistContentService(
		map[discovery.ProviderName]discovery.ArtistContentProvider{
			discovery.ProviderDeezer:     topTracks(discovery.TrackFrom(discovery.ProviderDeezer, "d-l", "Lithium", "Nirvana")),
			discovery.ProviderSoundCloud: topTracks(discovery.TrackFrom(discovery.ProviderSoundCloud, "sc-r", "Rainbow Chaser", "Nirvana")),
		},
		discovery.WithContentIdentityStore(&discovery.FakeIdentityStore{}),
	)

	resp, err := svc.GetTopTracks(t.Context(), discovery.ProviderDeezer, "d1", map[discovery.ProviderName]string{discovery.ProviderSoundCloud: "sc1"}, 5)
	if err != nil {
		t.Fatal(err)
	}

	if len(resp.Items) != 1 || resp.Items[0].Title != "Lithium" {
		t.Fatalf("items = %+v, want only Lithium", resp.Items)
	}
}

func countingEmptyProvider(albumCalls, trackCalls *int) *discovery.FakeArtistContentProvider {
	return &discovery.FakeArtistContentProvider{
		GetAlbumsFn: func(_ context.Context, _ discovery.ProviderName, _ string) ([]discovery.SearchResult, error) {
			*albumCalls++
			return nil, nil
		},
		GetTopTracksFn: func(_ context.Context, _ discovery.ProviderName, _ string) ([]discovery.SearchResult, error) {
			*trackCalls++
			return nil, nil
		},
	}
}

func TestGetArtistContent_v2_anEmptyAnswerIsNotFoundWithoutAskingAgain(t *testing.T) {
	var albumCalls, trackCalls int
	svc := discovery.NewGetArtistContentService(
		map[discovery.ProviderName]discovery.ArtistContentProvider{discovery.ProviderDeezer: countingEmptyProvider(&albumCalls, &trackCalls)},
		discovery.WithContentIdentityStore(&discovery.FakeIdentityStore{}),
	)

	albums, err := svc.GetAlbums(t.Context(), discovery.ProviderDeezer, "d1", nil, 50)
	if err != nil {
		t.Fatal(err)
	}
	tracks, err := svc.GetTopTracks(t.Context(), discovery.ProviderDeezer, "d1", nil, 10)
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
	mixedPage := func() []discovery.SearchResult {
		page := make([]discovery.SearchResult, 0, 24)
		for i := 1; i <= 20; i++ {
			page = append(page, discovery.V2Album(discovery.ProviderDeezer, "d-o"+strings.Repeat("i", i), "other "+strings.Repeat("i", i)))
		}
		for _, title := range mbTitles[:4] {
			page = append(page, discovery.V2Album(discovery.ProviderDeezer, "d-"+title, title))
		}
		return page
	}
	cleanPage := func() []discovery.SearchResult {
		page := make([]discovery.SearchResult, 0, 10)
		for _, title := range mbTitles[:10] {
			page = append(page, discovery.V2Album(discovery.ProviderDeezer, "d-"+title, title))
		}
		return page
	}
	deezerOnly := func() []discovery.SearchResult {
		wtf := discovery.TrackFrom(discovery.ProviderDeezer, "d-wtf", "wtf", "Che")
		wtf.Album = "other i"
		hugo := discovery.TrackFrom(discovery.ProviderDeezer, "d-hugo", "hugo", "Che")
		hugo.Album = "other ii"
		return []discovery.SearchResult{wtf, hugo}
	}
	tests := []struct {
		name   string
		albums []discovery.SearchResult
		anchor stubMBAnchor
		want   []string
	}{
		{"mixed page drops its deezer-only tracks", mixedPage(), stubMBAnchor{titles: mbTitles}, []string{}},
		{"clean page keeps every track", cleanPage(), stubMBAnchor{titles: mbTitles}, []string{"wtf", "hugo"}},
		{"MB error returns the tracks unfiltered", mixedPage(), stubMBAnchor{err: errors.New("musicbrainz down")}, []string{"wtf", "hugo"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			deezer := &discovery.FakeArtistContentProvider{
				GetAlbumsFn: func(context.Context, discovery.ProviderName, string) ([]discovery.SearchResult, error) {
					return tc.albums, nil
				},
				GetTopTracksFn: func(context.Context, discovery.ProviderName, string) ([]discovery.SearchResult, error) {
					return deezerOnly(), nil
				},
			}
			svc := discovery.NewGetArtistContentService(
				map[discovery.ProviderName]discovery.ArtistContentProvider{discovery.ProviderDeezer: deezer},
				discovery.WithContentIdentityStore(&discovery.FakeIdentityStore{Mbid: "mbid-che", Xref: map[discovery.ProviderKey]string{"deezer": "d1"}}),
				discovery.WithMBAnchor(tc.anchor),
			)

			resp, err := svc.GetTopTracks(t.Context(), discovery.ProviderDeezer, "d1", nil, 10)
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
