package service

import (
	"context"
	"log/slog"
	"sort"
	"strconv"
	"sync"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared/textnorm"
)

type GetArtistContentService struct {
	providers     map[domain.ProviderName]ports.ArtistContentProvider
	consensus     *ConsensusService
	identityStore ports.IdentityStore
	mbAnchor      ports.MBDiscographyAnchor
}

func NewGetArtistContentService(
	providers map[domain.ProviderName]ports.ArtistContentProvider,
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

// WithContentIdentityStore attaches the durable identity store the identity-first
// V2 detail path reverse-resolves a single provider id into the artist's full
// cross-provider identity through. Its presence is what enables that path; without
// it the service serves the single seed provider directly. (Named apart from the
// search service's WithIdentityStore — same package.)
func WithContentIdentityStore(store ports.IdentityStore) ArtistContentOption {
	return func(s *GetArtistContentService) { s.identityStore = store }
}

// WithMBAnchor gives the V2 discography its identity-verification anchor: the
// MusicBrainz release-group set each fan-out provider is checked against, so a
// mis-bridged same-name artist (doc §7) is dropped. Optional — without it, V2
// skips MB verification and relies on the cohesion fallback.
func WithMBAnchor(anchor ports.MBDiscographyAnchor) ArtistContentOption {
	return func(s *GetArtistContentService) { s.mbAnchor = anchor }
}

func (s *GetArtistContentService) GetTopTracks(ctx context.Context, providerName domain.ProviderName, externalID, artistName string, limit int) (*ContentFetchResponse, error) {
	if s.identityStore != nil {
		// Identity-first V2: resolve the artist's full cross-provider identity and fan
		// out by each provider's own id. Runs on the seed even without a durable bridge
		// (the seed id is already id-verified); an empty fan-out falls through to the
		// single-provider path below. Gated on the store because V2 requires it — with
		// no store the service just serves the seed provider directly.
		identity, _ := resolveArtistIdentity(ctx, s.identityStore, providerName, externalID)
		if tracks := s.v2TopTracks(ctx, identity, artistName); len(tracks) > 0 {
			return okContentResponse(providerName, tracks, limit), nil
		}
	}

	provider, ok := s.providers[providerName]
	if !ok {
		return errorContentResponse(providerName), nil
	}
	results, degraded := fetchProviderResults(ctx, providerName, externalID, "artist_top_tracks.provider_failed",
		func(ctx context.Context, pn domain.ProviderName, id string) ([]domain.SearchResult, error) {
			return provider.GetArtistTopTracks(ctx, pn, id)
		})
	if degraded != nil {
		return degraded, nil
	}
	return okContentResponse(providerName, results, limit), nil
}

// identityContentFetch is one provider's content call (top-tracks or albums),
// already resolved to this provider's own id for the artist.
type identityContentFetch func(ctx context.Context, p ports.ArtistContentProvider, provider domain.ProviderName, externalID string) ([]domain.SearchResult, error)

// detailFanOutTimeout bounds the identity fan-out (same principled SLA as
// consensusTimeout): one hung provider must not hold the artist-detail request
// open to the server timeout. A var (not const) only so tests can shrink it.
var detailFanOutTimeout = consensusTimeout

// fanOutByIdentity asks every content provider that has a resolved id for THIS
// artist to fetch concurrently, returning one non-empty result group per
// responder. Each provider is queried by its OWN id (or the MBID for Last.fm,
// which keys top-tracks on it) — never a name — so a same-name artist can't bleed
// in. A provider with no resolved id, or one that errors, simply doesn't
// contribute.
func (s *GetArtistContentService) fanOutByIdentity(ctx context.Context, identity ResolvedArtistIdentity, artistName string, fetch identityContentFetch) [][]domain.SearchResult {
	ctx, cancel := context.WithTimeout(ctx, detailFanOutTimeout)
	defer cancel()

	type job struct {
		provider domain.ProviderName
		p        ports.ArtistContentProvider
		id       string
	}
	var jobs []job
	// Canonical provider order, not map range: the MergeReleases cluster
	// incumbent is first-seen, so a per-request-random order flips the displayed
	// title/year for the same album between requests (the same determinism bug
	// class as the search artwork flicker).
	for _, name := range orderedProviderNames(s.providers) {
		p := s.providers[name]
		id := providerContentID(identity, name)
		if id == "" {
			// The cross-provider identity (MB url-relations) carries no id for
			// this provider — SoundCloud is never bridged. If the provider can
			// resolve one from the artist name, use that single resolved id so
			// its underground-exclusive catalogue still joins the fan-out.
			id = resolveArtistIDByName(ctx, p, artistName)
		}
		if id == "" {
			continue
		}
		jobs = append(jobs, job{provider: name, p: p, id: id})
	}

	groups := make([][]domain.SearchResult, len(jobs))
	var wg sync.WaitGroup
	for i, j := range jobs {
		wg.Add(1)
		go func(i int, j job) {
			defer wg.Done()
			res, err := fetch(ctx, j.p, j.provider, j.id)
			if err != nil {
				// Degrade-on-error stays — the provider just doesn't contribute.
				slog.DebugContext(ctx, "artist_content.fanout.provider_failed",
					"provider", j.provider.String(), "error", err)
				return
			}
			groups[i] = res
		}(i, j)
	}
	wg.Wait()

	out := make([][]domain.SearchResult, 0, len(groups))
	for _, g := range groups {
		if len(g) > 0 {
			out = append(out, g)
		}
	}
	return out
}

func (s *GetArtistContentService) GetAlbums(ctx context.Context, providerName domain.ProviderName, externalID, artistName string, limit int) (*ContentFetchResponse, error) {
	if s.identityStore != nil {
		// Identity-first V2 (see GetTopTracks): resolve the cross-provider identity and
		// fan out by each provider's own id, gated on the store V2 requires. Runs on
		// the seed even without a durable bridge; empty → single-provider path below.
		identity, _ := resolveArtistIdentity(ctx, s.identityStore, providerName, externalID)
		if albums := s.v2Albums(ctx, identity, artistName); len(albums) > 0 {
			return okContentResponse(providerName, albums, limit), nil
		}
	}

	provider, ok := s.providers[providerName]
	if !ok {
		return errorContentResponse(providerName), nil
	}
	results, degraded := fetchProviderResults(ctx, providerName, externalID, "artist_albums.provider_failed",
		func(ctx context.Context, pn domain.ProviderName, id string) ([]domain.SearchResult, error) {
			return provider.GetArtistAlbums(ctx, pn, id)
		})
	if degraded != nil {
		return degraded, nil
	}

	results = dedupAlbums(results)

	if artistName != "" && s.consensus != nil {
		consensusResults := s.consensus.BuildConsensus(ctx, artistName, providerName, externalID, results)
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

	return okContentResponse(providerName, results, limit), nil
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

// providerFanOutPriority is the canonical provider order for the detail
// fan-out — richest-metadata providers first, so they win first-seen ties in
// MergeReleases. Providers not listed sort alphabetically after.
var providerFanOutPriority = []domain.ProviderName{
	domain.ProviderDeezer,
	domain.ProviderAppleMusic,
	domain.ProviderSpotify,
	domain.ProviderITunes,
	domain.ProviderSoundCloud,
	domain.ProviderLastFM,
}

// orderedProviderNames returns the provider map's keys in a fixed deterministic
// order: providerFanOutPriority first, everything else alphabetically.
func orderedProviderNames(providers map[domain.ProviderName]ports.ArtistContentProvider) []domain.ProviderName {
	out := make([]domain.ProviderName, 0, len(providers))
	seen := make(map[domain.ProviderName]bool, len(providers))
	for _, name := range providerFanOutPriority {
		if _, ok := providers[name]; ok {
			out = append(out, name)
			seen[name] = true
		}
	}
	var rest []domain.ProviderName
	for name := range providers {
		if !seen[name] {
			rest = append(rest, name)
		}
	}
	sort.Slice(rest, func(i, j int) bool { return rest[i].String() < rest[j].String() })
	return append(out, rest...)
}
