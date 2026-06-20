package service

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"altune/go-api/internal/discovery/domain"
)

const (
	consensusTimeout           = 2 * time.Second
	consensusTitleMatchMinTSR  = 85
	consensusMinRespondedCount = 4
)

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
	mb        mbLookup
}

type ConsensusOption func(*ConsensusService)

func WithConsensusMB(mb mbLookup) ConsensusOption {
	return func(s *ConsensusService) { s.mb = mb }
}

func NewConsensusService(providers []ConsensusProvider, opts ...ConsensusOption) *ConsensusService {
	s := &ConsensusService{providers: providers}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *ConsensusService) BuildConsensus(
	ctx context.Context,
	artistName string,
	primaryAlbums []domain.SearchResult,
) []ConsensusAlbum {
	if len(primaryAlbums) == 0 {
		return nil
	}

	otherProviderAlbums := s.fetchFromProviders(ctx, artistName)

	respondedCount := 0
	for _, albums := range otherProviderAlbums {
		if albums != nil {
			respondedCount++
		}
	}

	slog.InfoContext(ctx, "consensus.providers_responded",
		"artist", artistName,
		"responded", respondedCount,
		"total_providers", len(s.providers),
	)

	results := make([]ConsensusAlbum, 0, len(primaryAlbums))
	for _, album := range primaryAlbums {
		matchCount := s.countMatches(album, otherProviderAlbums)
		status, reason := classifyAlbum(matchCount, respondedCount)
		results = append(results, ConsensusAlbum{
			Album:  annotateConsensus(album, status, matchCount, respondedCount),
			Status: status,
			Reason: reason,
		})
	}

	results = s.applyMBContradiction(ctx, artistName, primaryAlbums, results)

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
		"confirmed", confirmed,
		"unconfirmed", unconfirmed,
		"rejected", rejected,
	)

	return results
}

func (s *ConsensusService) fetchFromProviders(ctx context.Context, artistName string) map[string][]domain.SearchResult {
	ctx, cancel := context.WithTimeout(ctx, consensusTimeout)
	defer cancel()

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
			result[provider.Name] = albums
		}(p)
	}

	wg.Wait()
	return result
}

func (s *ConsensusService) countMatches(album domain.SearchResult, otherProviderAlbums map[string][]domain.SearchResult) int {
	albumNorm := NormalizeForMatch(album.Title)
	matchCount := 0

	for _, albums := range otherProviderAlbums {
		if albums == nil {
			continue
		}
		for _, other := range albums {
			otherNorm := NormalizeForMatch(other.Title)
			if TokenSortRatio(albumNorm, otherNorm) >= consensusTitleMatchMinTSR {
				matchCount++
				break
			}
		}
	}

	return matchCount
}

func classifyAlbum(matchCount, respondedCount int) (ConsensusStatus, string) {
	if matchCount >= 1 {
		return ConsensusConfirmed, "found on multiple providers"
	}
	if respondedCount >= consensusMinRespondedCount {
		return ConsensusRejected, "not found on any other provider with sufficient consensus"
	}
	return ConsensusUnconfirmed, "insufficient provider data to confirm"
}

func (s *ConsensusService) applyMBContradiction(
	ctx context.Context,
	artistName string,
	primaryAlbums []domain.SearchResult,
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

	validated, err := s.mb.ValidateArtistAlbums(ctx, artistName, primaryAlbums)
	if err != nil || validated == nil {
		return results
	}

	confirmedTitles := make(map[string]bool, len(validated.Confirmed))
	for _, a := range validated.Confirmed {
		confirmedTitles[NormalizeForMatch(a.Title)] = true
	}

	if len(confirmedTitles) == 0 && len(primaryAlbums) >= 4 {
		slog.InfoContext(ctx, "consensus.mb_identity_discarded",
			"artist", artistName,
			"reason", "zero overlap between MB confirmed titles and primary albums",
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
