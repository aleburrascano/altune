package service

import (
	"context"
	"math"
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
	identityFirst bool
	discographyV2 bool
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
// path reverse-resolves a single provider id into the artist's full cross-provider
// identity through. Without it, the identity-first path degrades to single-provider.
// (Named apart from the search service's WithIdentityStore — same package.)
func WithContentIdentityStore(store ports.IdentityStore) ArtistContentOption {
	return func(s *GetArtistContentService) { s.identityStore = store }
}

// WithIdentityFirst turns on the identity-first detail path (DETAIL_IDENTITY_FIRST):
// fan out across every content provider by the artist's OWN id per provider and
// merge, instead of trusting a single provider id. Falls back to the single-
// provider path whenever the identity can't be resolved.
func WithIdentityFirst() ArtistContentOption {
	return func(s *GetArtistContentService) { s.identityFirst = true }
}

// WithDiscographyV2 turns on the rebuilt discography core (DISCOGRAPHY_V2, doc §6):
// best-of merge → confidence-keep → record-type-normalize, replacing the lossy
// Merge+consensus+MB-veto path. Only takes effect on the identity-first path.
func WithDiscographyV2() ArtistContentOption {
	return func(s *GetArtistContentService) { s.discographyV2 = true }
}

func (s *GetArtistContentService) GetTopTracks(ctx context.Context, providerName domain.ProviderName, externalID, artistName string, limit int) (*ContentFetchResponse, error) {
	if s.identityFirst {
		identity, ok := resolveArtistIdentity(ctx, s.identityStore, providerName, externalID)
		// V2 runs on the seed identity alone (see GetAlbums); the old path needs ok.
		if s.discographyV2 {
			if tracks := s.v2TopTracks(ctx, identity, artistName); len(tracks) > 0 {
				return okContentResponse(providerName, tracks, limit), nil
			}
		} else if ok {
			if tracks := s.identityTopTracks(ctx, identity, artistName); len(tracks) > 0 {
				return okContentResponse(providerName, tracks, limit), nil
			}
			// Empty fan-out (every provider missed): fall through to the single-
			// provider path rather than showing an empty top-tracks list.
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

// identityTopTracks fans top-tracks out across every provider that has a resolved
// id for this artist, merges by track identity, and orders most-corroborated
// first. A same-name artist's tracks arrive from at most one provider (its wrong
// id), so they never corroborate and sink below the real, multi-source tracks —
// which is how "Agenda"/"Miley Cyrus"-style bleed gets pushed out.
func (s *GetArtistContentService) identityTopTracks(ctx context.Context, identity ResolvedArtistIdentity, artistName string) []domain.SearchResult {
	groups := s.fanOutByIdentity(ctx, identity, artistName, func(ctx context.Context, p ports.ArtistContentProvider, provider domain.ProviderName, id string) ([]domain.SearchResult, error) {
		return p.GetArtistTopTracks(ctx, provider, id)
	})
	entities := Merge(groups)
	sortByAgreement(entities)
	out := make([]domain.SearchResult, 0, len(entities))
	for _, e := range entities {
		out = append(out, e.Result)
	}
	return out
}

// identityContentFetch is one provider's content call (top-tracks or albums),
// already resolved to this provider's own id for the artist.
type identityContentFetch func(ctx context.Context, p ports.ArtistContentProvider, provider domain.ProviderName, externalID string) ([]domain.SearchResult, error)

// fanOutByIdentity asks every content provider that has a resolved id for THIS
// artist to fetch concurrently, returning one non-empty result group per
// responder. Each provider is queried by its OWN id (or the MBID for Last.fm,
// which keys top-tracks on it) — never a name — so a same-name artist can't bleed
// in. A provider with no resolved id, or one that errors, simply doesn't
// contribute.
func (s *GetArtistContentService) fanOutByIdentity(ctx context.Context, identity ResolvedArtistIdentity, artistName string, fetch identityContentFetch) [][]domain.SearchResult {
	type job struct {
		provider domain.ProviderName
		p        ports.ArtistContentProvider
		id       string
	}
	var jobs []job
	for name, p := range s.providers {
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
			if res, err := fetch(ctx, j.p, j.provider, j.id); err == nil {
				groups[i] = res
			}
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

// sortByAgreement orders merged entities most-corroborated first: more providers
// that returned it (agreement) beats fewer, then the best native position across
// providers breaks the tie. Stable so equal-agreement entities keep merge order.
func sortByAgreement(entities []Entity) {
	sort.SliceStable(entities, func(i, j int) bool {
		ai, aj := len(entities[i].BestRank), len(entities[j].BestRank)
		if ai != aj {
			return ai > aj
		}
		return bestRankOf(entities[i]) < bestRankOf(entities[j])
	})
}

func bestRankOf(e Entity) int {
	best := math.MaxInt
	for _, r := range e.BestRank {
		if r < best {
			best = r
		}
	}
	return best
}

func (s *GetArtistContentService) GetAlbums(ctx context.Context, providerName domain.ProviderName, externalID, artistName string, limit int) (*ContentFetchResponse, error) {
	if s.identityFirst {
		identity, ok := resolveArtistIdentity(ctx, s.identityStore, providerName, externalID)
		// V2 runs on the SEED identity alone — the seed provider id is already
		// id-verified (it IS this artist for that provider), so V2 works even when
		// the durable store has no cross-provider bridge (the common case for
		// underground artists MusicBrainz doesn't url-relate). The old identity-
		// first path still requires a resolved bridge (ok).
		if s.discographyV2 {
			if albums := s.v2Albums(ctx, identity, artistName); len(albums) > 0 {
				return okContentResponse(providerName, albums, limit), nil
			}
		} else if ok {
			if albums := s.identityAlbums(ctx, identity, artistName); len(albums) > 0 {
				return okContentResponse(providerName, albums, limit), nil
			}
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

	return okContentResponse(providerName, results, limit), nil
}

// identityAlbums builds the discography identity-first: fetch each provider's
// albums by the artist's OWN id for that provider, merge best-of (a cover or year
// from ANY source fills the gap), keep consensus for MB-spine completeness, then
// drop the metadata-less noise that used to render as broken cards. Fixes (C):
// same-name albums can't bleed in, and every surviving album has a cover or a year.
func (s *GetArtistContentService) identityAlbums(ctx context.Context, identity ResolvedArtistIdentity, artistName string) []domain.SearchResult {
	groups := s.fanOutByIdentity(ctx, identity, artistName, func(ctx context.Context, p ports.ArtistContentProvider, provider domain.ProviderName, id string) ([]domain.SearchResult, error) {
		return p.GetArtistAlbums(ctx, provider, id)
	})
	entities := Merge(groups)
	albums := make([]domain.SearchResult, 0, len(entities))
	for _, e := range entities {
		albums = append(albums, e.Result)
	}

	// Completeness: consensus adds MB-spine albums the id fan-out didn't reach,
	// anchored on the identity-safe primary set above.
	if artistName != "" && s.consensus != nil {
		var kept []domain.SearchResult
		for _, cr := range s.consensus.BuildConsensus(ctx, artistName, albums) {
			if cr.Status != ConsensusRejected {
				kept = append(kept, cr.Album)
			}
		}
		albums = kept
	}

	normalizeAlbumYears(albums)
	albums = hideBareAlbums(albums)
	sortAlbumsByReleaseDateDesc(albums)
	return albums
}

// hideBareAlbums drops the metadata-less noise the discography used to render as
// broken cards: an album with no cover AND no year AND only a single source. Any
// album with an image, a known year, or multi-source corroboration is kept.
func hideBareAlbums(albums []domain.SearchResult) []domain.SearchResult {
	out := albums[:0]
	for _, a := range albums {
		bare := a.ImageURL == "" && a.Year == 0 && len(a.Sources) <= 1
		if !bare {
			out = append(out, a)
		}
	}
	return out
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
