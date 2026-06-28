package service

import (
	"context"
	"log/slog"
	"sort"
	"strconv"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared/textnorm"
)

type GetArtistContentService struct {
	providers map[string]ports.ArtistContentProvider
	consensus *ConsensusService
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

func WithConsensusService(c *ConsensusService) ArtistContentOption {
	return func(s *GetArtistContentService) { s.consensus = c }
}

func (s *GetArtistContentService) GetTopTracks(ctx context.Context, providerName, externalID string, limit int) (*ContentFetchResponse, error) {
	provider, ok := s.providers[providerName]
	if !ok {
		return &ContentFetchResponse{
			ProviderName: providerName,
			Status:       domain.ProviderStatusError,
			Items:        []domain.SearchResult{},
		}, nil
	}

	pn, err := domain.ParseProviderName(providerName)
	if err != nil {
		return &ContentFetchResponse{
			ProviderName: providerName,
			Status:       domain.ProviderStatusError,
			Items:        []domain.SearchResult{},
		}, nil
	}
	results, err := provider.GetArtistTopTracks(ctx, pn, externalID)
	if err != nil {
		slog.WarnContext(ctx, "artist_top_tracks.provider_failed",
			"provider", providerName, "external_id", externalID, "error", err)
		return &ContentFetchResponse{
			ProviderName: providerName,
			Status:       domain.ProviderStatusError,
			Items:        []domain.SearchResult{},
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
			Items:        []domain.SearchResult{},
		}, nil
	}

	pn, err := domain.ParseProviderName(providerName)
	if err != nil {
		return &ContentFetchResponse{
			ProviderName: providerName,
			Status:       domain.ProviderStatusError,
			Items:        []domain.SearchResult{},
		}, nil
	}
	results, err := provider.GetArtistAlbums(ctx, pn, externalID)
	if err != nil {
		slog.WarnContext(ctx, "artist_albums.provider_failed",
			"provider", providerName, "external_id", externalID, "error", err)
		return &ContentFetchResponse{
			ProviderName: providerName,
			Status:       domain.ProviderStatusError,
			Items:        []domain.SearchResult{},
		}, nil
	}

	results = dedupAlbums(results)

	if artistName != "" && s.consensus != nil {
		consensusResults := s.consensus.BuildConsensus(ctx, artistName, results)
		var kept []domain.SearchResult
		for _, cr := range consensusResults {
			if cr.Status != ConsensusRejected {
				kept = append(kept, cr.Album)
			}
		}
		if kept == nil {
			kept = []domain.SearchResult{}
		}
		results = kept
	}

	// The backend owns discography ordering and year display: normalize a numeric
	// year from each album's release date, then sort newest-first BEFORE truncating
	// so the limit keeps the newest releases. The client just displays the result.
	normalizeAlbumYears(results)
	sortAlbumsByReleaseDateDesc(results)

	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}

	return &ContentFetchResponse{
		ProviderName: providerName,
		Status:       domain.ProviderStatusOK,
		Items:        results,
	}, nil
}

func dedupAlbums(results []domain.SearchResult) []domain.SearchResult {
	seen := make(map[string]int)
	var deduped []domain.SearchResult

	for _, r := range results {
		normTitle := textnorm.NormalizeForMatch(r.Title) + "|" + textnorm.NormalizeForMatch(r.Subtitle)
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
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}

// sortAlbumsByReleaseDateDesc orders albums newest-first by their release-date
// sort key. Albums with no usable date (e.g. Last.fm, which carries none) sort to
// the end. Stable so equal-date albums keep dedup order.
func sortAlbumsByReleaseDateDesc(results []domain.SearchResult) {
	sort.SliceStable(results, func(i, j int) bool {
		ki, kj := albumReleaseSortKey(results[i]), albumReleaseSortKey(results[j])
		if ki == "" || kj == "" {
			// Missing keys sink to the end; present-before-absent.
			return ki != "" && kj == ""
		}
		return ki > kj // ISO dates / years are lexicographically descending = newest-first
	})
}

// albumReleaseSortKey returns the comparable date string for ordering: the ISO
// release_date when present (Deezer/iTunes), else the bare year, else "".
func albumReleaseSortKey(r domain.SearchResult) string {
	if r.Extras == nil {
		return ""
	}
	if rd, ok := r.Extras["release_date"].(string); ok && rd != "" {
		return rd
	}
	return yearString(r.Extras["year"])
}

// normalizeAlbumYears derives a numeric extras["year"] from release_date for any
// album missing one, so the client always has a year to display without parsing
// dates itself. Idempotent; leaves albums with no date untouched.
func normalizeAlbumYears(results []domain.SearchResult) {
	for i := range results {
		if results[i].Extras == nil {
			continue
		}
		if _, ok := results[i].Extras["year"]; ok {
			continue
		}
		rd, ok := results[i].Extras["release_date"].(string)
		if !ok || len(rd) < 4 {
			continue
		}
		if y := parseYear(rd[:4]); y > 0 {
			results[i].Extras["year"] = y
		}
	}
}

// yearString renders an extras["year"] value (int/int64/float64/string) as a bare
// year string for sorting, or "" when absent/unparseable.
func yearString(v any) string {
	switch y := v.(type) {
	case int:
		if y > 0 {
			return strconv.Itoa(y)
		}
	case int64:
		if y > 0 {
			return strconv.FormatInt(y, 10)
		}
	case float64:
		if y > 0 {
			return strconv.Itoa(int(y))
		}
	case string:
		if len(y) >= 4 {
			return y[:4]
		}
	}
	return ""
}

// parseYear parses a 4-char year prefix into a positive int, or 0 if invalid.
func parseYear(s string) int {
	y, err := strconv.Atoi(s)
	if err != nil || y <= 0 {
		return 0
	}
	return y
}
