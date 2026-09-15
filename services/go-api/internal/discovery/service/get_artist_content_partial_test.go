package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"context"
	"testing"
)

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
	providers := make(map[domain.ProviderName]ports.ArtistContentProvider, len(everyContentProvider))
	xref := make(map[string]string, len(everyContentProvider))
	for _, name := range everyContentProvider {
		xref[name.String()] = "id-" + name.String()
		providers[name] = &fakeArtistContentProvider{
			getTopTracksFn: func(_ context.Context, pn domain.ProviderName, id string) ([]domain.SearchResult, error) {
				if fail(pn) {
					return nil, upstreamDown
				}
				return []domain.SearchResult{trackFrom(pn, id, "Real Song", "Che")}, nil
			},
			getAlbumsFn: func(_ context.Context, pn domain.ProviderName, id string) ([]domain.SearchResult, error) {
				if fail(pn) {
					return nil, upstreamDown
				}
				return []domain.SearchResult{v2Album(pn, id, "Fully Loaded", withDate("2026-04-01"))}, nil
			},
		}
	}
	store := &fakeIdentityStore{mbid: "mbid-che", xref: xref}
	return NewGetArtistContentService(providers, append(opts, WithContentIdentityStore(store))...)
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
