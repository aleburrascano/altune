package service

import (
	"context"
	"log/slog"
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared/textnorm"
)

// --- local interfaces for adapter dependencies ---

// mbLookup is the subset of MusicBrainzAdapter the resolver needs.
type mbLookup interface {
	LookupAlbumArtist(ctx context.Context, artistName, albumTitle string, profile domain.ArtistIdentityProfile) (domain.AlbumVerdict, string, error)
	ResolveArtistIdentity(ctx context.Context, name string) (*ports.ArtistIdentity, error)
	ValidateArtistAlbums(ctx context.Context, artistName string, albums []domain.SearchResult) (*ports.AlbumValidationResult, error)
}

// itunesLookup is the subset of ITunesAdapter the resolver needs.
type itunesLookup interface {
	LookupAlbum(ctx context.Context, albumTitle, artistName string, profile domain.ArtistIdentityProfile) (domain.AlbumVerdict, error)
}

// isrcFetcher fetches the ISRC for a track by provider-specific ID.
type isrcFetcher interface {
	FetchTrackISRC(ctx context.Context, trackID string) (string, error)
}

// identityCache stores and retrieves per-album identity verdicts.
type identityCache interface {
	GetVerdict(ctx context.Context, artistName, albumTitle string) (verdict domain.AlbumVerdict, reason, layer string, firstSeen time.Time, ok bool)
	SetVerdict(ctx context.Context, artistName, albumTitle string, verdict domain.AlbumVerdict, reason, layer string)
}

// IdentityResolverService orchestrates R2->R3->R3b->R3c identity
// resolution layers with short-circuiting, caching, and graceful
// degradation on provider errors.
type IdentityResolverService struct {
	mb          mbLookup
	itunes      itunesLookup
	isrc        isrcFetcher
	cache       identityCache // nil-safe
}

type IdentityResolverOption func(*IdentityResolverService)

func WithMBLookup(mb mbLookup) IdentityResolverOption {
	return func(s *IdentityResolverService) { s.mb = mb }
}

func WithITunesLookup(it itunesLookup) IdentityResolverOption {
	return func(s *IdentityResolverService) { s.itunes = it }
}

func WithISRCFetcher(f isrcFetcher) IdentityResolverOption {
	return func(s *IdentityResolverService) { s.isrc = f }
}

func WithIdentityCache(c identityCache) IdentityResolverOption {
	return func(s *IdentityResolverService) { s.cache = c }
}

func NewIdentityResolverService(opts ...IdentityResolverOption) *IdentityResolverService {
	s := &IdentityResolverService{}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// BuildProfile assembles an ArtistIdentityProfile from MB data and
// album extras. Call once before Resolve.
func (s *IdentityResolverService) BuildProfile(
	ctx context.Context,
	artistName string,
	albums []domain.SearchResult,
) domain.ArtistIdentityProfile {
	profile := domain.NewArtistIdentityProfile()

	// Resolve MB identity (MBID, birth year, disambiguation)
	if s.mb != nil {
		identity, err := s.mb.ResolveArtistIdentity(ctx, artistName)
		if err != nil {
			slog.WarnContext(ctx, "identity.resolve_artist_failed",
				"artist", artistName, "error", err)
		} else if identity != nil {
			profile.MBID = identity.MBID
			profile.BirthYear = identity.BirthYear
			profile.Disambiguation = identity.Disambiguation
		}
	}

	// Collect genre cluster from album extras
	for _, album := range albums {
		genres := extractAlbumGenres(album)
		for _, g := range genres {
			profile.AddGenre(g)
		}
	}

	return profile
}

// Resolve runs the identity resolution pipeline for each album:
// cache -> MB confirmed set -> R2 (MB reverse-lookup) -> R3 (iTunes) ->
// R3b (constraints) -> R3c (ISRC registrant).
// Returns an AlbumResolution per album with verdict, reason, and layer.
func (s *IdentityResolverService) Resolve(
	ctx context.Context,
	artistName string,
	profile domain.ArtistIdentityProfile,
	albums []domain.SearchResult,
) []ports.AlbumResolution {
	// Build the MB confirmed set once for the whole batch.
	mbConfirmed := map[string]bool{}
	if s.mb != nil {
		validated, err := s.mb.ValidateArtistAlbums(ctx, artistName, albums)
		if err != nil {
			slog.WarnContext(ctx, "identity.validate_albums_failed",
				"artist", artistName, "error", err)
		} else {
			for _, a := range validated.Confirmed {
				mbConfirmed[textnorm.NormalizeForMatch(a.Title)] = true
			}
		}
	}

	resolutions := make([]ports.AlbumResolution, 0, len(albums))
	for _, album := range albums {
		res := s.resolveOne(ctx, artistName, profile, album, mbConfirmed)
		resolutions = append(resolutions, res)
	}
	return resolutions
}

func (s *IdentityResolverService) resolveOne(
	ctx context.Context,
	artistName string,
	profile domain.ArtistIdentityProfile,
	album domain.SearchResult,
	mbConfirmed map[string]bool,
) ports.AlbumResolution {
	titleNorm := textnorm.NormalizeForMatch(album.Title)

	// 1. Cache check
	if s.cache != nil {
		verdict, reason, layer, _, ok := s.cache.GetVerdict(ctx, artistName, album.Title)
		if ok {
			slog.DebugContext(ctx, "identity.cache_hit",
				"artist", artistName, "album", album.Title,
				"verdict", verdict.String())
			return ports.AlbumResolution{
				Album:   album,
				Verdict: verdict,
				Reason:  reason,
				Layer:   layer,
			}
		}
	}

	// 2. Already confirmed by MB validation (release-group membership)
	if mbConfirmed[titleNorm] {
		s.cacheVerdict(ctx, artistName, album.Title, domain.AlbumVerdictConfirmed, "mb release-group match", "mb")
		return ports.AlbumResolution{
			Album:   album,
			Verdict: domain.AlbumVerdictConfirmed,
			Reason:  "mb release-group match",
			Layer:   "mb",
		}
	}

	// 3. R2: MB reverse-lookup per album
	if s.mb != nil && profile.MBID != "" {
		verdict, _, err := s.mb.LookupAlbumArtist(ctx, artistName, album.Title, profile)
		if err == nil && verdict != domain.AlbumVerdictUnknown {
			reason := "mb reverse-lookup"
			if verdict == domain.AlbumVerdictContamination {
				reason = "mb credited to different artist"
			}
			s.cacheVerdict(ctx, artistName, album.Title, verdict, reason, "mb")
			return ports.AlbumResolution{
				Album:   album,
				Verdict: verdict,
				Reason:  reason,
				Layer:   "mb",
			}
		}
	}

	// 4. R3: iTunes cross-provider search
	if s.itunes != nil {
		verdict, err := s.itunes.LookupAlbum(ctx, album.Title, artistName, profile)
		if err == nil && verdict != domain.AlbumVerdictUnknown {
			reason := "itunes cross-reference confirmed"
			if verdict == domain.AlbumVerdictContamination {
				reason = "itunes credited to different artist or incompatible genre"
			}
			s.cacheVerdict(ctx, artistName, album.Title, verdict, reason, "itunes")
			return ports.AlbumResolution{
				Album:   album,
				Verdict: verdict,
				Reason:  reason,
				Layer:   "itunes",
			}
		}
	}

	// 5. R3b: Profile constraint checks
	if CheckTemporalImpossibility(profile, album) {
		s.cacheVerdict(ctx, artistName, album.Title, domain.AlbumVerdictContamination, "album predates artist activity", "temporal")
		return ports.AlbumResolution{
			Album:   album,
			Verdict: domain.AlbumVerdictContamination,
			Reason:  "album predates artist activity",
			Layer:   "temporal",
		}
	}
	if CheckGenreIncompatibility(profile, album) {
		s.cacheVerdict(ctx, artistName, album.Title, domain.AlbumVerdictContamination, "genre incompatible with artist profile", "genre")
		return ports.AlbumResolution{
			Album:   album,
			Verdict: domain.AlbumVerdictContamination,
			Reason:  "genre incompatible with artist profile",
			Layer:   "genre",
		}
	}
	if CheckArtistTypeMismatch(profile, album) {
		s.cacheVerdict(ctx, artistName, album.Title, domain.AlbumVerdictContamination, "artist type mismatch", "type")
		return ports.AlbumResolution{
			Album:   album,
			Verdict: domain.AlbumVerdictContamination,
			Reason:  "artist type mismatch",
			Layer:   "type",
		}
	}

	// 6. R3c: ISRC registrant fingerprint
	if s.isrc != nil && len(profile.KnownISRCRegistrants) > 0 {
		trackID := extractFirstTrackID(album)
		if trackID != "" {
			isrc, err := s.isrc.FetchTrackISRC(ctx, trackID)
			if err == nil && isrc != "" {
				if CheckISRCRegistrantMismatch(profile, isrc) {
					// Check if this is a re-evaluation (firstSeen > 24h ago)
					verdict := domain.AlbumVerdictSuspect
					reason := "isrc registrant mismatch (first encounter)"
					if s.cache != nil {
						_, _, _, firstSeen, cached := s.cache.GetVerdict(ctx, artistName, album.Title)
						if cached && !firstSeen.IsZero() && time.Since(firstSeen) > 24*time.Hour {
							verdict = domain.AlbumVerdictContamination
							reason = "isrc registrant mismatch (confirmed after 24h)"
						}
					}
					s.cacheVerdict(ctx, artistName, album.Title, verdict, reason, "isrc")
					return ports.AlbumResolution{
						Album:   album,
						Verdict: verdict,
						Reason:  reason,
						Layer:   "isrc",
					}
				}
			}
		}
	}

	// 7. Default: unknown (optimistic include)
	s.cacheVerdict(ctx, artistName, album.Title, domain.AlbumVerdictUnknown, "no definitive signals", "")
	return ports.AlbumResolution{
		Album:   album,
		Verdict: domain.AlbumVerdictUnknown,
		Reason:  "no definitive signals",
		Layer:   "",
	}
}

func (s *IdentityResolverService) cacheVerdict(ctx context.Context, artistName, albumTitle string, verdict domain.AlbumVerdict, reason, layer string) {
	if s.cache == nil {
		return
	}
	s.cache.SetVerdict(ctx, artistName, albumTitle, verdict, reason, layer)
}

// extractFirstTrackID tries to find a Deezer track ID from the album's
// sources or tracklist extras. Returns empty string if none found.
func extractFirstTrackID(album domain.SearchResult) string {
	// Try tracklist extras (a list of track IDs set during enrichment)
	if album.Extras != nil {
		if tl, ok := album.Extras["tracklist"].([]any); ok && len(tl) > 0 {
			if id, ok := tl[0].(string); ok {
				return id
			}
		}
		// Try deezer_album_id to construct a track lookup
		if albumID, ok := album.Extras["deezer_album_id"].(string); ok && albumID != "" {
			return "" // can't get track ID from album ID alone
		}
	}
	// Try the first source if it's from Deezer
	for _, src := range album.Sources {
		if src.Provider == domain.ProviderDeezer {
			return src.ExternalID
		}
	}
	return ""
}
