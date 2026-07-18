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
		return errorContentResponse(providerName), nil
	}

	pn, err := domain.ParseProviderName(providerName)
	if err != nil {
		return errorContentResponse(providerName), nil
	}
	results, err := provider.GetArtistTopTracks(ctx, pn, externalID)
	if err != nil {
		slog.WarnContext(ctx, "artist_top_tracks.provider_failed",
			"provider", providerName, "external_id", externalID, "error", err)
		return errorContentResponse(providerName), nil
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
		return errorContentResponse(providerName), nil
	}

	pn, err := domain.ParseProviderName(providerName)
	if err != nil {
		return errorContentResponse(providerName), nil
	}
	results, err := provider.GetArtistAlbums(ctx, pn, externalID)
	if err != nil {
		slog.WarnContext(ctx, "artist_albums.provider_failed",
			"provider", providerName, "external_id", externalID, "error", err)
		return errorContentResponse(providerName), nil
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
			if r.TrackCount > deduped[idx].TrackCount {
				deduped[idx] = r
			}
			continue
		}
		seen[normTitle] = len(deduped)
		deduped = append(deduped, r)
	}
	return deduped
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
// ReleaseDate when present (Deezer/iTunes), else the bare year, else "".
func albumReleaseSortKey(r domain.SearchResult) string {
	if r.ReleaseDate != "" {
		return r.ReleaseDate
	}
	if r.Year > 0 {
		return strconv.Itoa(r.Year)
	}
	return ""
}

// normalizeAlbumYears derives a numeric Year from ReleaseDate for any album
// missing one, so the client always has a year to display without parsing dates
// itself. Idempotent; leaves albums with no date untouched.
func normalizeAlbumYears(results []domain.SearchResult) {
	for i := range results {
		if results[i].Year != 0 || len(results[i].ReleaseDate) < 4 {
			continue
		}
		if y := parseYear(results[i].ReleaseDate[:4]); y > 0 {
			results[i].Year = y
		}
	}
}

// parseYear parses a 4-char year prefix into a positive int, or 0 if invalid.
func parseYear(s string) int {
	y, err := strconv.Atoi(s)
	if err != nil || y <= 0 {
		return 0
	}
	return y
}
