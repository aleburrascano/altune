package service

import (
	"context"
	"errors"
	"testing"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
)

func TestGetArtistContentService_GetTopTracks(t *testing.T) {
	sampleTracks := []domain.SearchResult{
		{Kind: domain.ResultKindTrack, Title: "Blinding Lights", Subtitle: "The Weeknd", Sources: []domain.SourceRef{{Provider: domain.ProviderDeezer, ExternalID: "t1"}}},
		{Kind: domain.ResultKindTrack, Title: "Save Your Tears", Subtitle: "The Weeknd", Sources: []domain.SourceRef{{Provider: domain.ProviderDeezer, ExternalID: "t2"}}},
		{Kind: domain.ResultKindTrack, Title: "Starboy", Subtitle: "The Weeknd", Sources: []domain.SourceRef{{Provider: domain.ProviderDeezer, ExternalID: "t3"}}},
	}

	tests := []struct {
		name          string
		providerName  string
		externalID    string
		limit         int
		providers     map[string]ports.ArtistContentProvider
		wantStatus    domain.ProviderStatus
		wantItemCount int
	}{
		{
			name:         "valid provider returns top tracks",
			providerName: "deezer",
			externalID:   "artist-42",
			limit:        0,
			providers: map[string]ports.ArtistContentProvider{
				"deezer": &fakeArtistContentProvider{
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
			providerName:  "nonexistent",
			externalID:    "artist-42",
			limit:         0,
			providers:     map[string]ports.ArtistContentProvider{},
			wantStatus:    domain.ProviderStatusError,
			wantItemCount: 0,
		},
		{
			name:         "provider error returns error status",
			providerName: "deezer",
			externalID:   "artist-err",
			limit:        0,
			providers: map[string]ports.ArtistContentProvider{
				"deezer": &fakeArtistContentProvider{
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
			providerName: "deezer",
			externalID:   "artist-42",
			limit:        2,
			providers: map[string]ports.ArtistContentProvider{
				"deezer": &fakeArtistContentProvider{
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

			resp, err := svc.GetTopTracks(context.Background(), tt.providerName, tt.externalID, tt.limit)

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
		providerName  string
		externalID    string
		limit         int
		providers     map[string]ports.ArtistContentProvider
		wantStatus    domain.ProviderStatus
		wantItemCount int
	}{
		{
			name:         "valid provider returns albums",
			providerName: "deezer",
			externalID:   "artist-42",
			limit:        0,
			providers: map[string]ports.ArtistContentProvider{
				"deezer": &fakeArtistContentProvider{
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
			providerName:  "nonexistent",
			externalID:    "artist-42",
			limit:         0,
			providers:     map[string]ports.ArtistContentProvider{},
			wantStatus:    domain.ProviderStatusError,
			wantItemCount: 0,
		},
		{
			name:         "dedup by normalized title keeps album with higher track_count",
			providerName: "deezer",
			externalID:   "artist-42",
			limit:        0,
			providers: map[string]ports.ArtistContentProvider{
				"deezer": &fakeArtistContentProvider{
					getAlbumsFn: func(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
						return []domain.SearchResult{
							{
								Kind:    domain.ResultKindAlbum,
								Title:   "After Hours",
								Sources: []domain.SourceRef{{Provider: domain.ProviderDeezer, ExternalID: "a1"}},
								Extras:  map[string]any{"track_count": 14},
							},
							{
								Kind:    domain.ResultKindAlbum,
								Title:   "After Hours (Deluxe)",
								Sources: []domain.SourceRef{{Provider: domain.ProviderDeezer, ExternalID: "a2"}},
								Extras:  map[string]any{"track_count": 18},
							},
							{
								Kind:    domain.ResultKindAlbum,
								Title:   "Starboy",
								Sources: []domain.SourceRef{{Provider: domain.ProviderDeezer, ExternalID: "a3"}},
								Extras:  map[string]any{"track_count": 18},
							},
						}, nil
					},
				},
			},
			wantStatus:    domain.ProviderStatusOK,
			wantItemCount: 2, // "After Hours" and "After Hours (Deluxe)" normalize the same → deduped to 1, plus "Starboy" = 2
		},
		{
			name:         "dedup keeps higher track_count version",
			providerName: "deezer",
			externalID:   "artist-42",
			limit:        0,
			providers: map[string]ports.ArtistContentProvider{
				"deezer": &fakeArtistContentProvider{
					getAlbumsFn: func(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
						return []domain.SearchResult{
							{
								Kind:    domain.ResultKindAlbum,
								Title:   "Dawn FM",
								Sources: []domain.SourceRef{{Provider: domain.ProviderDeezer, ExternalID: "a1"}},
								Extras:  map[string]any{"track_count": 10},
							},
							{
								Kind:    domain.ResultKindAlbum,
								Title:   "Dawn FM (Alternate World)",
								Sources: []domain.SourceRef{{Provider: domain.ProviderDeezer, ExternalID: "a2"}},
								Extras:  map[string]any{"track_count": 20},
							},
						}, nil
					},
				},
			},
			wantStatus:    domain.ProviderStatusOK,
			wantItemCount: 1, // same normalized title → keep the one with higher track_count
		},
		{
			name:         "provider error returns error status",
			providerName: "deezer",
			externalID:   "artist-err",
			limit:        0,
			providers: map[string]ports.ArtistContentProvider{
				"deezer": &fakeArtistContentProvider{
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
		extras := map[string]any{}
		if releaseDate != "" {
			extras["release_date"] = releaseDate
		}
		return domain.SearchResult{
			Kind:    domain.ResultKindAlbum,
			Title:   title,
			Sources: []domain.SourceRef{{Provider: domain.ProviderDeezer, ExternalID: extID}},
			Extras:  extras,
		}
	}

	t.Run("sorts newest-first and normalizes year from release_date", func(t *testing.T) {
		providers := map[string]ports.ArtistContentProvider{
			"deezer": &fakeArtistContentProvider{
				getAlbumsFn: func(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
					// Deliberately out of order.
					return []domain.SearchResult{
						album("Older", "2016-09-09", "a1"),
						album("Newest", "2022-01-07", "a3"),
						album("Middle", "2020-03-20", "a2"),
					}, nil
				},
			},
		}
		svc := NewGetArtistContentService(providers)

		resp, err := svc.GetAlbums(context.Background(), "deezer", "artist-1", "", 0)
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
			if y, ok := got.Extras["year"].(int); !ok || y != wantYear[want] {
				t.Errorf("%q: expected year %d, got %v", want, wantYear[want], got.Extras["year"])
			}
		}
	})

	t.Run("albums with no date sort to the end", func(t *testing.T) {
		providers := map[string]ports.ArtistContentProvider{
			"deezer": &fakeArtistContentProvider{
				getAlbumsFn: func(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
					return []domain.SearchResult{
						album("NoDate", "", "a1"),
						album("Dated", "2019-05-01", "a2"),
					}, nil
				},
			},
		}
		svc := NewGetArtistContentService(providers)

		resp, err := svc.GetAlbums(context.Background(), "deezer", "artist-1", "", 0)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp.Items) != 2 || resp.Items[0].Title != "Dated" || resp.Items[1].Title != "NoDate" {
			t.Errorf("expected [Dated, NoDate], got %v", albumTitles(resp.Items))
		}
	})

	t.Run("limit keeps the newest after sorting", func(t *testing.T) {
		providers := map[string]ports.ArtistContentProvider{
			"deezer": &fakeArtistContentProvider{
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

		resp, err := svc.GetAlbums(context.Background(), "deezer", "artist-1", "", 2)
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
