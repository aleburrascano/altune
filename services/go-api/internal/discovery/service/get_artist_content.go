package service

import (
	"context"
	"log/slog"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
)

type GetArtistContentService struct {
	providers        map[string]ports.ArtistContentProvider
	identityResolver *IdentityResolverService
}

func NewGetArtistContentService(
	providers map[string]ports.ArtistContentProvider,
	opts ...ArtistContentOption,
) *GetArtistContentService {
	s := &GetArtistContentService{providers: providers}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

type ArtistContentOption func(*GetArtistContentService)

func WithArtistIdentityResolver(r *IdentityResolverService) ArtistContentOption {
	return func(s *GetArtistContentService) { s.identityResolver = r }
}

func (s *GetArtistContentService) GetTopTracks(ctx context.Context, providerName, externalID string, limit int) (*ContentFetchResponse, error) {
	provider, ok := s.providers[providerName]
	if !ok {
		return &ContentFetchResponse{
			ProviderName: providerName,
			Status:       domain.ProviderStatusError,
		}, nil
	}

	pn, err := domain.ParseProviderName(providerName)
	if err != nil {
		return &ContentFetchResponse{
			ProviderName: providerName,
			Status:       domain.ProviderStatusError,
		}, nil
	}
	results, err := provider.GetArtistTopTracks(ctx, pn, externalID)
	if err != nil {
		return &ContentFetchResponse{
			ProviderName: providerName,
			Status:       domain.ProviderStatusError,
		}, nil
	}

	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}

	return &ContentFetchResponse{
		ProviderName: providerName,
		Status:       domain.ProviderStatusOK,
		Items:        results,
	}, nil
}

func (s *GetArtistContentService) GetAlbums(ctx context.Context, providerName, externalID, artistName string, limit int) (*ContentFetchResponse, error) {
	provider, ok := s.providers[providerName]
	if !ok {
		return &ContentFetchResponse{
			ProviderName: providerName,
			Status:       domain.ProviderStatusError,
		}, nil
	}

	pn, err := domain.ParseProviderName(providerName)
	if err != nil {
		return &ContentFetchResponse{
			ProviderName: providerName,
			Status:       domain.ProviderStatusError,
		}, nil
	}
	results, err := provider.GetArtistAlbums(ctx, pn, externalID)
	if err != nil {
		return &ContentFetchResponse{
			ProviderName: providerName,
			Status:       domain.ProviderStatusError,
		}, nil
	}

	results = dedupAlbums(results)

	if artistName != "" && s.identityResolver != nil {
		results = s.resolveDiscographyIdentity(ctx, artistName, results)
	}

	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}

	return &ContentFetchResponse{
		ProviderName: providerName,
		Status:       domain.ProviderStatusOK,
		Items:        results,
	}, nil
}

// resolveDiscographyIdentity runs the identity resolution pipeline and
// returns confirmed albums first, then unknown, with contamination removed.
func (s *GetArtistContentService) resolveDiscographyIdentity(ctx context.Context, artistName string, results []domain.SearchResult) []domain.SearchResult {
	profile := s.identityResolver.BuildProfile(ctx, artistName, results)
	resolutions := s.identityResolver.Resolve(ctx, artistName, profile, results)

	var confirmed, unknown []domain.SearchResult
	removedCount := 0
	for _, res := range resolutions {
		switch res.Verdict {
		case domain.AlbumVerdictConfirmed:
			confirmed = append(confirmed, res.Album)
		case domain.AlbumVerdictContamination:
			removedCount++
			slog.DebugContext(ctx, "identity.album_removed",
				"artist", artistName, "album", res.Album.Title,
				"reason", res.Reason, "layer", res.Layer)
		default:
			// Unknown and Suspect are kept (optimistic include)
			unknown = append(unknown, res.Album)
		}
	}

	if removedCount > 0 {
		slog.InfoContext(ctx, "identity.resolution_applied",
			"artist", artistName,
			"confirmed", len(confirmed),
			"unknown", len(unknown),
			"removed", removedCount,
		)
	}

	// Confirmed first, then unknown
	out := make([]domain.SearchResult, 0, len(confirmed)+len(unknown))
	out = append(out, confirmed...)
	out = append(out, unknown...)
	return out
}

func dedupAlbums(results []domain.SearchResult) []domain.SearchResult {
	seen := make(map[string]int)
	var deduped []domain.SearchResult

	for _, r := range results {
		normTitle := NormalizeForMatch(r.Title)
		if idx, ok := seen[normTitle]; ok {
			existingCount := getTrackCount(deduped[idx])
			newCount := getTrackCount(r)
			if newCount > existingCount {
				deduped[idx] = r
			}
			continue
		}
		seen[normTitle] = len(deduped)
		deduped = append(deduped, r)
	}
	return deduped
}

func getTrackCount(r domain.SearchResult) int {
	if r.Extras == nil {
		return 0
	}
	v, ok := r.Extras["track_count"]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	default:
		return 0
	}
}
