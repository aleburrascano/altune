package service

import (
	"context"
	"log/slog"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
)

type GetArtistContentService struct {
	providers      map[string]ports.ArtistContentProvider
	albumValidator ports.AlbumValidator
	discogsEnrich  ports.DiscographyEnricher
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

func WithAlbumValidator(v ports.AlbumValidator) ArtistContentOption {
	return func(s *GetArtistContentService) { s.albumValidator = v }
}

func WithDiscogsEnricher(d ports.DiscographyEnricher) ArtistContentOption {
	return func(s *GetArtistContentService) { s.discogsEnrich = d }
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

	if artistName != "" && (s.albumValidator != nil || s.discogsEnrich != nil) {
		results = s.validateAndFilterDiscography(ctx, artistName, results)
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

func (s *GetArtistContentService) validateAndFilterDiscography(ctx context.Context, artistName string, results []domain.SearchResult) []domain.SearchResult {
	mbConfirmed := make(map[string]bool)
	discogsConfirmed := make(map[string]bool)
	birthYear := 0

	if s.albumValidator != nil {
		identity, _ := s.albumValidator.ResolveArtistIdentity(ctx, artistName)
		if identity != nil {
			birthYear = identity.BirthYear
		}

		validated, err := s.albumValidator.ValidateArtistAlbums(ctx, artistName, results)
		if err != nil {
			slog.WarnContext(ctx, "album_validation_failed", "artist", artistName, "error", err)
		} else {
			for _, a := range validated.Confirmed {
				mbConfirmed[NormalizeForMatch(a.Title)] = true
			}
			slog.InfoContext(ctx, "album_validation_applied",
				"artist", artistName,
				"confirmed", len(validated.Confirmed),
				"unconfirmed", len(validated.Unconfirmed),
			)
		}
	}

	if s.discogsEnrich != nil {
		results = s.applyDiscogsEnrichment(ctx, artistName, results, discogsConfirmed)
	}

	return FilterContamination(results, DiscographyFilterInput{
		BirthYear:        birthYear,
		MBConfirmed:      mbConfirmed,
		DiscogsConfirmed: discogsConfirmed,
	})
}

func (s *GetArtistContentService) applyDiscogsEnrichment(ctx context.Context, artistName string, results []domain.SearchResult, discogsConfirmed map[string]bool) []domain.SearchResult {
	albumTitles := make([]string, len(results))
	for i, r := range results {
		albumTitles[i] = r.Title
	}
	discogsArtist, err := s.discogsEnrich.ResolveDiscogsArtist(ctx, artistName, albumTitles)
	if err != nil {
		slog.WarnContext(ctx, "discogs_enrichment_failed", "artist", artistName, "error", err)
		return results
	}
	if discogsArtist == nil {
		return results
	}
	if discogsArtist.Overlap == 0 {
		slog.InfoContext(ctx, "discogs_enrichment_skipped",
			"artist", artistName,
			"discogs_id", discogsArtist.ID,
			"reason", "zero album overlap, unreliable match")
		return results
	}

	releases, err := s.discogsEnrich.FetchArtistReleases(ctx, discogsArtist.ID)
	if err != nil {
		slog.WarnContext(ctx, "discogs_releases_failed", "artist", artistName, "error", err)
		return results
	}

	discogsYears := make(map[string]int, len(releases))
	for _, rel := range releases {
		norm := NormalizeForMatch(rel.Title)
		discogsConfirmed[norm] = true
		if rel.Year > 0 {
			discogsYears[norm] = rel.Year
		}
	}
	for i, r := range results {
		if extractYear(r) == 0 {
			norm := NormalizeForMatch(r.Title)
			if y, ok := discogsYears[norm]; ok {
				extras := copyExtras(r.Extras)
				extras["year"] = y
				results[i].Extras = extras
			}
		}
	}
	slog.InfoContext(ctx, "discogs_enrichment_applied",
		"artist", artistName,
		"discogs_id", discogsArtist.ID,
		"overlap", discogsArtist.Overlap,
		"releases", len(releases),
		"years_backfilled", len(discogsYears),
	)
	return results
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
