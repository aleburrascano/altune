package service

import (
	"context"
	"log/slog"
	"sync"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
)

// mbConsensusLookup is the subset of MusicBrainzAdapter the consensus service needs.
type mbConsensusLookup interface {
	LookupAlbumArtist(ctx context.Context, artistName, albumTitle string, profile domain.ArtistIdentityProfile) (domain.AlbumVerdict, string, error)
	ResolveArtistIdentity(ctx context.Context, name string) (*ports.ArtistIdentity, error)
	ValidateArtistAlbums(ctx context.Context, artistName string, albums []domain.SearchResult) (*ports.AlbumValidationResult, error)
}

const consensusTitleMatchMinTSR = 85

type ConsensusStatus string

const (
	ConsensusConfirmed   ConsensusStatus = "confirmed"
	ConsensusUnconfirmed ConsensusStatus = "unconfirmed"
	ConsensusRejected    ConsensusStatus = "rejected"
)

type ConsensusAlbum struct {
	Album  domain.SearchResult
	Status ConsensusStatus
	Reason string
}

type ConsensusProvider struct {
	Name    string
	Fetcher func(ctx context.Context, artistName string) ([]domain.SearchResult, error)
}

type ConsensusService struct {
	providers []ConsensusProvider
	mb        mbConsensusLookup
}

type ConsensusOption func(*ConsensusService)

func WithConsensusMB(mb mbConsensusLookup) ConsensusOption {
	return func(s *ConsensusService) { s.mb = mb }
}

func NewConsensusService(providers []ConsensusProvider, opts ...ConsensusOption) *ConsensusService {
	s := &ConsensusService{providers: providers}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// BuildConsensus merges albums from ALL providers into a union.
// Every provider is an equal source — no single provider is "primary."
// Albums appearing on 2+ providers are confirmed. Albums on 1 provider
// are unconfirmed but included. MB contradiction is the only removal.
// No hardcoded timeout — uses the request's context deadline.
func (s *ConsensusService) BuildConsensus(
	ctx context.Context,
	artistName string,
	primaryAlbums []domain.SearchResult,
) []ConsensusAlbum {
	allProviderAlbums := s.fetchFromProviders(ctx, artistName)

	respondedCount := 0
	for _, albums := range allProviderAlbums {
		if albums != nil {
			respondedCount++
		}
	}

	slog.InfoContext(ctx, "consensus.providers_responded",
		"artist", artistName,
		"responded", respondedCount,
		"total_providers", len(s.providers),
	)

	// AIDEV-DECISION: merge ALL providers' albums into a union, not just
	// validate one provider's list. This way OsamaSon gets albums from
	// Tidal + Last.fm even when Deezer has sparse data.
	type mergedAlbum struct {
		album      domain.SearchResult
		providerCount int
		providers  []string
	}

	merged := make(map[string]*mergedAlbum)
	var mergeOrder []string

	addAlbum := func(album domain.SearchResult, providerName string) {
		titleNorm := NormalizeForMatch(album.Title)
		for key, existing := range merged {
			if TokenSortRatio(titleNorm, key) >= consensusTitleMatchMinTSR {
				existing.providerCount++
				existing.providers = append(existing.providers, providerName)
				if completeness(album) > completeness(existing.album) {
					existing.album = album
				}
				return
			}
		}
		merged[titleNorm] = &mergedAlbum{
			album:      album,
			providerCount: 1,
			providers:  []string{providerName},
		}
		mergeOrder = append(mergeOrder, titleNorm)
	}

	for _, album := range primaryAlbums {
		addAlbum(album, "deezer")
	}
	for provName, albums := range allProviderAlbums {
		if albums == nil {
			continue
		}
		for _, album := range albums {
			addAlbum(album, provName)
		}
	}

	results := make([]ConsensusAlbum, 0, len(merged))
	for _, key := range mergeOrder {
		entry := merged[key]
		status := ConsensusUnconfirmed
		reason := "single provider"
		if entry.providerCount >= 2 {
			status = ConsensusConfirmed
			reason = "found on multiple providers"
		}
		results = append(results, ConsensusAlbum{
			Album:  annotateConsensus(entry.album, status, entry.providerCount, respondedCount),
			Status: status,
			Reason: reason,
		})
	}

	// Add albums from other providers that didn't match any primary album
	// (these are albums Deezer doesn't have but other providers do)
	for provName, albums := range allProviderAlbums {
		if albums == nil {
			continue
		}
		for _, album := range albums {
			titleNorm := NormalizeForMatch(album.Title)
			found := false
			for key := range merged {
				if TokenSortRatio(titleNorm, key) >= consensusTitleMatchMinTSR {
					found = true
					break
				}
			}
			if !found {
				merged[titleNorm] = &mergedAlbum{
					album:      album,
					providerCount: 1,
					providers:  []string{provName},
				}
				results = append(results, ConsensusAlbum{
					Album:  annotateConsensus(album, ConsensusUnconfirmed, 1, respondedCount),
					Status: ConsensusUnconfirmed,
					Reason: "from " + provName + " only",
				})
			}
		}
	}

	results = s.applyMBContradiction(ctx, artistName, results)

	confirmed, unconfirmed, rejected := 0, 0, 0
	for _, r := range results {
		switch r.Status {
		case ConsensusConfirmed:
			confirmed++
		case ConsensusUnconfirmed:
			unconfirmed++
		case ConsensusRejected:
			rejected++
		}
	}
	slog.InfoContext(ctx, "consensus.complete",
		"artist", artistName,
		"total", len(results),
		"confirmed", confirmed,
		"unconfirmed", unconfirmed,
		"rejected", rejected,
	)

	return results
}

func (s *ConsensusService) fetchFromProviders(ctx context.Context, artistName string) map[string][]domain.SearchResult {
	var mu sync.Mutex
	result := make(map[string][]domain.SearchResult, len(s.providers))
	var wg sync.WaitGroup

	for _, p := range s.providers {
		wg.Add(1)
		go func(provider ConsensusProvider) {
			defer wg.Done()
			albums, err := provider.Fetcher(ctx, artistName)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				slog.DebugContext(ctx, "consensus.provider_failed",
					"provider", provider.Name,
					"artist", artistName,
					"error", err,
				)
				result[provider.Name] = nil
				return
			}
			if len(albums) > 0 {
				slog.DebugContext(ctx, "consensus.provider_responded",
					"provider", provider.Name,
					"artist", artistName,
					"albums", len(albums),
				)
			}
			result[provider.Name] = albums
		}(p)
	}

	wg.Wait()
	return result
}

func (s *ConsensusService) applyMBContradiction(
	ctx context.Context,
	artistName string,
	results []ConsensusAlbum,
) []ConsensusAlbum {
	if s.mb == nil {
		return results
	}

	profile := domain.NewArtistIdentityProfile()
	identity, err := s.mb.ResolveArtistIdentity(ctx, artistName)
	if err != nil || identity == nil || identity.MBID == "" {
		return results
	}
	profile.MBID = identity.MBID

	allAlbums := make([]domain.SearchResult, len(results))
	for i, r := range results {
		allAlbums[i] = r.Album
	}

	validated, err := s.mb.ValidateArtistAlbums(ctx, artistName, allAlbums)
	if err != nil || validated == nil {
		return results
	}

	confirmedTitles := make(map[string]bool, len(validated.Confirmed))
	for _, a := range validated.Confirmed {
		confirmedTitles[NormalizeForMatch(a.Title)] = true
	}

	if len(confirmedTitles) == 0 && len(allAlbums) >= 4 {
		slog.InfoContext(ctx, "consensus.mb_identity_discarded",
			"artist", artistName,
			"reason", "zero overlap between MB confirmed titles and albums",
		)
		return results
	}

	mbCallCount := 0
	for i, result := range results {
		if result.Status == ConsensusRejected {
			continue
		}
		titleNorm := NormalizeForMatch(result.Album.Title)
		if confirmedTitles[titleNorm] {
			if results[i].Status != ConsensusConfirmed {
				results[i].Status = ConsensusConfirmed
				results[i].Reason = "confirmed by MusicBrainz"
				results[i].Album = annotateConsensus(results[i].Album, ConsensusConfirmed, 1, 0)
			}
			continue
		}

		if mbCallCount >= 10 {
			continue
		}
		mbCallCount++
		verdict, _, lookupErr := s.mb.LookupAlbumArtist(ctx, artistName, result.Album.Title, profile)
		if lookupErr != nil {
			continue
		}
		if verdict == domain.AlbumVerdictContamination {
			results[i].Status = ConsensusRejected
			results[i].Reason = "MusicBrainz credits to different artist"
			results[i].Album = annotateConsensus(results[i].Album, ConsensusRejected, 0, 0)
			slog.DebugContext(ctx, "consensus.mb_contradiction",
				"artist", artistName,
				"album", result.Album.Title,
			)
		}
	}

	return results
}

func annotateConsensus(album domain.SearchResult, status ConsensusStatus, matchCount, respondedCount int) domain.SearchResult {
	extras := copyExtras(album.Extras)
	extras["consensus_status"] = string(status)
	if matchCount > 0 {
		extras["consensus_matches"] = matchCount
	}
	if respondedCount > 0 {
		extras["consensus_responded"] = respondedCount
	}
	album.Extras = extras
	return album
}
