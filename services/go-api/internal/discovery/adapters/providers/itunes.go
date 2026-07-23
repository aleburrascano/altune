package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/textnorm"
)

type ITunesAdapter struct {
	client *http.Client
	mu     sync.Mutex
	tat    time.Time // GCRA theoretical arrival time of the next conforming request
}

func NewITunesAdapter(client *http.Client) *ITunesAdapter {
	return &ITunesAdapter{client: client}
}

// iTunes rate limiting (GCRA token bucket). Apple's ceiling is ~20 req/min/IP —
// exceeding it returns HTTP 403 (not 503). itunesEmitInterval sets the sustained
// rate (~15 req/min, safely under the ceiling); itunesBurst lets a short run of
// calls fire back-to-back before throttling kicks in. The burst is what makes a
// single search usable: its 3 sequential kind calls fire immediately instead of
// being spaced 3.5s apart (the old fixed-gap limiter blew the search SLA and made
// iTunes time out on every query), while sustained load still stays under 403 range.
const (
	itunesEmitInterval = 4 * time.Second // sustained ~15 req/min
	itunesBurst        = 4               // calls allowed back-to-back before spacing
)

// itunesUserAgent identifies the client to Apple. The default Go user-agent
// (Go-http-client/1.1) makes Apple's abuse heuristic stricter; a plain
// identifying UA avoids that.
const itunesUserAgent = "Altune/1.0 (music manager; self-hosted)"

// rateLimit blocks until the GCRA limiter admits a request. tat is the
// theoretical arrival time of the next conforming request: a call is admitted
// immediately while now is within the burst tolerance of tat, otherwise it waits
// until then. Each admitted call pushes tat forward by one emit interval.
//
// The limiter is per-adapter. iTunes is constructed as several instances
// (search, consensus, artwork, content) that don't share this gate, but each
// reflects a separate flow; for a personal/family-scale deployment the
// per-instance budget is sufficient.
func (a *ITunesAdapter) rateLimit(ctx context.Context) {
	const burstTolerance = time.Duration(itunesBurst-1) * itunesEmitInterval

	a.mu.Lock()
	now := time.Now()
	if a.tat.Before(now) {
		a.tat = now
	}
	wait := time.Until(a.tat.Add(-burstTolerance))
	a.tat = a.tat.Add(itunesEmitInterval)
	a.mu.Unlock()

	if wait <= 0 {
		return
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
	}
}

// SearchTimeout overrides the fan-out default. iTunes runs three sequential
// kind searches (~3s total at limit=200); the burst-tolerant limiter lets them
// fire without spacing, but the 1.5s default is still too tight for three calls.
func (a *ITunesAdapter) SearchTimeout() time.Duration { return 4 * time.Second }

func (a *ITunesAdapter) Name() domain.ProviderName { return domain.ProviderITunes }

func (a *ITunesAdapter) SupportedKinds() map[domain.ResultKind]bool {
	return map[domain.ResultKind]bool{
		domain.ResultKindTrack:  true,
		domain.ResultKindAlbum:  true,
		domain.ResultKindArtist: true,
	}
}

func (a *ITunesAdapter) Search(ctx context.Context, query string, kinds map[domain.ResultKind]bool) ([]domain.SearchResult, error) {
	return searchAcrossKinds(ctx, "itunes", query, kinds, a.SupportedKinds(),
		func(ctx context.Context, kind domain.ResultKind) ([]domain.SearchResult, error) {
			return a.searchKind(ctx, query, kind)
		})
}

func (a *ITunesAdapter) searchKind(ctx context.Context, query string, kind domain.ResultKind) ([]domain.SearchResult, error) {
	entity := itunesEntity(kind)
	// limit=200 is the API max (default 50) — deeper recall for merge at no extra
	// rate cost (same single call). The fan-out trims to the requested limit.
	u := fmt.Sprintf("https://itunes.apple.com/search?term=%s&entity=%s&country=US&limit=200", url.QueryEscape(query), entity)

	a.rateLimit(ctx)
	var body itunesResponse
	if err := getJSON(ctx, a.client, u, &body, withHeader("User-Agent", itunesUserAgent)); err != nil {
		return nil, err
	}

	var results []domain.SearchResult
	for _, item := range body.Results {
		results = append(results, mapITunesResult(item, kind))
	}
	return results, nil
}

func itunesEntity(kind domain.ResultKind) string {
	switch kind {
	case domain.ResultKindTrack:
		return "song"
	case domain.ResultKindAlbum:
		return "album"
	case domain.ResultKindArtist:
		return "musicArtist"
	default:
		return "song"
	}
}

func mapITunesResult(item itunesItem, kind domain.ResultKind) domain.SearchResult {
	artworkURL := upscaleArtwork(item.ArtworkURL100, iTunesListArtworkSize)

	extras := make(map[string]any)
	if item.TrackTimeMillis > 0 {
		extras["duration"] = item.TrackTimeMillis / 1000
	}
	if item.PrimaryGenreName != "" {
		extras["genre"] = item.PrimaryGenreName
	}

	var title, subtitle string
	switch kind {
	case domain.ResultKindTrack:
		title = item.TrackName
		subtitle = item.ArtistName
		extras["album"] = item.CollectionName
		if item.PreviewURL != "" {
			extras["preview_url"] = item.PreviewURL
		}
		if item.TrackNumber > 0 {
			extras["track_number"] = item.TrackNumber
		}
		if item.DiscNumber > 0 {
			extras["disc_number"] = item.DiscNumber
		}
		if item.TrackExplicitness == "explicit" {
			extras["explicit"] = true
		}
	case domain.ResultKindAlbum:
		title = stripAlbumTypeSuffix(item.CollectionName)
		subtitle = item.ArtistName
		if item.Copyright != "" {
			extras["copyright"] = item.Copyright
		}
		extras["record_type"] = iTunesRecordType(item.CollectionName)
	case domain.ResultKindArtist:
		title = item.ArtistName
	}

	externalID, sourceURL := itunesSourceRef(item, kind)
	r := domain.NewProviderResult(kind, title, subtitle, artworkURL,
		domain.SourceRef{Provider: domain.ProviderITunes, ExternalID: externalID, URL: sourceURL},
		extras)
	if kind == domain.ResultKindAlbum {
		r.TrackCount = item.TrackCount
		r.ReleaseDate = item.ReleaseDate
	}
	if kind == domain.ResultKindTrack {
		r.Album = item.CollectionName
		r.ReleaseDate = item.ReleaseDate // songs carry it too; only the album branch used to
		if item.TrackTimeMillis > 0 {
			r.Duration = int(item.TrackTimeMillis / 1000)
		}
	}
	return r
}

// itunesSourceRef returns the entity's own iTunes id + view URL for the kind:
// trackId/collectionId/artistId. Previously every kind carried trackId, which
// left album and artist results with an unusable "0" id (trackId is absent on
// those entities) — blocking the content lookups (cap 5). The change is
// merge-neutral: a SourceRef id only affects a merge decision through the
// xref-gated cross-provider bridge, and MusicBrainz url-relations never carry an
// Apple/iTunes id, so a real Apple id can never bridge-match.
func itunesSourceRef(item itunesItem, kind domain.ResultKind) (id, sourceURL string) {
	switch kind {
	case domain.ResultKindAlbum:
		return fmt.Sprintf("%d", item.CollectionID), item.CollectionViewURL
	case domain.ResultKindArtist:
		return fmt.Sprintf("%d", item.ArtistID), item.ArtistViewURL
	default:
		return fmt.Sprintf("%d", item.TrackID), item.TrackViewURL
	}
}

func upscaleArtwork(url string, size int) string {
	return strings.Replace(url, "100x100", fmt.Sprintf("%dx%d", size, size), 1)
}

// iTunesListArtworkSize is the resolution used for search-list thumbnails — a
// card-sized cover, kept modest to avoid bloating the search payload.
const iTunesListArtworkSize = 600

// iTunesHeroArtworkSize is the resolution used for the detail-open artwork
// fallback (the hero image). Apple serves resolution-templated artwork up to a
// real 3000×3000 (live-probed 2026-06-22 — see docs/providers/itunes.md §5.2),
// above Cover Art Archive's 1200px ceiling. We request 1500px: comfortably past
// CAA, but a fraction of the ~2.4MB a 3000px hero would cost on mobile data.
// The chain runs the MBID-keyed sources first, so this only fires on their miss.
const iTunesHeroArtworkSize = 1500

// Resolve implements ArtworkResolver — searches iTunes for cover art.
func (a *ITunesAdapter) Resolve(ctx context.Context, kind domain.ResultKind, title, subtitle string, mbid string) (string, error) {
	query := title
	if subtitle != "" {
		query = subtitle + " " + title
	}
	entity := itunesEntity(kind)

	u := fmt.Sprintf("https://itunes.apple.com/search?term=%s&entity=%s&country=US&limit=1", url.QueryEscape(query), entity)
	a.rateLimit(ctx)
	var body itunesResponse
	if err := getJSON(ctx, a.client, u, &body, withHeader("User-Agent", itunesUserAgent)); err != nil {
		return "", nil
	}
	for _, item := range body.Results {
		art := upscaleArtwork(item.ArtworkURL100, iTunesHeroArtworkSize)
		if art != "" {
			return art, nil
		}
	}
	return "", nil
}

// --- AlbumContentProvider + ArtistContentProvider (docs/providers/itunes.md cap 5) ---
//
// The iTunes /lookup endpoint returns the parent entity as the first result
// followed by its children (verified 2026-06-22): an artist+entity=album lookup
// returns the artist wrapper then its collections; a collection+entity=song
// lookup returns the collection wrapper then its tracks. We skip the parent
// wrapper (by wrapperType) and map the children. iTunes is a second mainstream
// source of truth for discography/tracklist alongside Deezer/MusicBrainz.

func (a *ITunesAdapter) GetAlbumTracks(ctx context.Context, _ domain.ProviderName, externalID string) ([]domain.SearchResult, error) {
	return a.lookupContent(ctx, externalID, "song")
}

// GetArtistTopTracks returns the artist's tracks via /lookup. iTunes lookup is
// catalog-ordered (recent-first), not popularity-ranked — unlike Deezer's
// /artist/{id}/top — so "top" here means "the artist's tracks", trimmed by the
// caller's limit.
func (a *ITunesAdapter) GetArtistTopTracks(ctx context.Context, _ domain.ProviderName, externalID string) ([]domain.SearchResult, error) {
	return a.lookupContent(ctx, externalID, "song")
}

func (a *ITunesAdapter) GetArtistAlbums(ctx context.Context, _ domain.ProviderName, externalID string) ([]domain.SearchResult, error) {
	return a.lookupContent(ctx, externalID, "album")
}

func (a *ITunesAdapter) lookupContent(ctx context.Context, id, entity string) ([]domain.SearchResult, error) {
	u := fmt.Sprintf(
		"https://itunes.apple.com/lookup?id=%s&entity=%s&country=US&limit=50",
		url.QueryEscape(id), entity,
	)
	a.rateLimit(ctx)
	var body itunesResponse
	if err := getJSON(ctx, a.client, u, &body, withHeader("User-Agent", itunesUserAgent)); err != nil {
		return nil, err
	}

	// Keep only the requested child wrapperType. This drops the parent wrapper
	// uniformly: an album→song lookup's parent is itself a "collection", so
	// filtering on wrapperType alone would leak it — we filter on the *target*
	// type the entity asked for (song→track, album→collection).
	targetWrapper, kind := itunesContentTarget(entity)
	results := make([]domain.SearchResult, 0, len(body.Results))
	for _, item := range body.Results {
		if item.WrapperType != targetWrapper {
			continue
		}
		results = append(results, mapITunesResult(item, kind))
	}
	return results, nil
}

// itunesContentTarget maps a /lookup entity param to the child wrapperType and
// ResultKind that lookup returns: entity=song → "track" children, entity=album
// → "collection" children.
func itunesContentTarget(entity string) (wrapperType string, kind domain.ResultKind) {
	if entity == "album" {
		return "collection", domain.ResultKindAlbum
	}
	return "track", domain.ResultKindTrack
}

// LookupAlbum searches iTunes for an album and returns a verdict on whether
// it belongs to the given artist based on name and genre compatibility.
// The returned int64 is the iTunes artist ID when confirmed (0 otherwise),
// used by the resolver for cross-album artist identity consistency.
func (a *ITunesAdapter) LookupAlbum(
	ctx context.Context,
	albumTitle, artistName string,
	profile domain.ArtistIdentityProfile,
) (domain.AlbumVerdict, int64, error) {
	u := fmt.Sprintf(
		"https://itunes.apple.com/search?term=%s&entity=album&country=US&limit=5",
		url.QueryEscape(albumTitle),
	)

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return domain.AlbumVerdictUnknown, 0, nil
	}
	req.Header.Set("User-Agent", itunesUserAgent)
	a.rateLimit(ctx)

	resp, err := a.client.Do(req)
	if err != nil {
		slog.WarnContext(ctx, "itunes.lookup_album_failed", "album", albumTitle, "error", err)
		return domain.AlbumVerdictUnknown, 0, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return domain.AlbumVerdictUnknown, 0, nil
	}

	var body itunesResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return domain.AlbumVerdictUnknown, 0, nil
	}

	titleNorm := textnorm.NormalizeForMatch(albumTitle)
	artistNorm := textnorm.NormalizeForMatch(artistName)

	for _, item := range body.Results {
		collNorm := textnorm.NormalizeForMatch(stripITunesTypeSuffix(item.CollectionName))
		if collNorm != titleNorm {
			continue
		}

		if textnorm.NormalizeForMatch(item.ArtistName) != artistNorm {
			return domain.AlbumVerdictContamination, item.ArtistID, nil
		}

		if len(profile.GenreCluster) > 0 && item.PrimaryGenreName != "" {
			genres := strings.Split(item.PrimaryGenreName, "/")
			if !profile.HasGenreOverlap(genres) {
				return domain.AlbumVerdictContamination, item.ArtistID, nil
			}
		}

		return domain.AlbumVerdictConfirmed, item.ArtistID, nil
	}

	return domain.AlbumVerdictUnknown, 0, nil
}

var itunesTypeSuffixes = []string{" - Single", " - EP", " - Album", " - Deluxe", " - Remix"}

func stripITunesTypeSuffix(name string) string {
	for _, suffix := range itunesTypeSuffixes {
		if idx := strings.Index(strings.ToLower(name), strings.ToLower(suffix)); idx >= 0 {
			return strings.TrimSpace(name[:idx])
		}
	}
	return name
}

// stripAlbumTypeSuffix removes a trailing " - Single"/" - EP" that Apple Music
// and iTunes append to album names, so "Fully Loaded - EP" displays as "Fully
// Loaded" AND clusters with the same album from other providers (a suffixed title
// would otherwise land in its own cluster). The record_type signal is derived
// separately (iTunesRecordType from the raw name; Apple's isSingle flag), so
// stripping the display title never loses it.
func stripAlbumTypeSuffix(title string) string {
	for _, suffix := range []string{" - Single", " - EP"} {
		if len(title) >= len(suffix) && strings.EqualFold(title[len(title)-len(suffix):], suffix) {
			return strings.TrimSpace(title[:len(title)-len(suffix)])
		}
	}
	return title
}

// iTunesRecordType derives an album's record type from the collection-name
// suffix iTunes appends (" - Single"/" - EP"). The Search API carries no clean
// type field, but the suffix is authoritative, so this is how the app tells a
// single/EP from an album for discography bucketing. Defaults to "album".
func iTunesRecordType(collectionName string) string {
	lower := strings.ToLower(collectionName)
	switch {
	case strings.Contains(lower, " - single"):
		return "single"
	case strings.Contains(lower, " - ep"):
		return "ep"
	default:
		return "album"
	}
}

type itunesResponse struct {
	Results []itunesItem `json:"results"`
}

type itunesItem struct {
	WrapperType       string `json:"wrapperType"`
	TrackID           int64  `json:"trackId"`
	TrackName         string `json:"trackName"`
	ArtistID          int64  `json:"artistId"`
	ArtistName        string `json:"artistName"`
	CollectionID      int64  `json:"collectionId"`
	CollectionName    string `json:"collectionName"`
	TrackViewURL      string `json:"trackViewUrl"`
	CollectionViewURL string `json:"collectionViewUrl"`
	ArtistViewURL     string `json:"artistViewUrl"`
	ArtworkURL100     string `json:"artworkUrl100"`
	PreviewURL        string `json:"previewUrl"`
	TrackTimeMillis   int64  `json:"trackTimeMillis"`
	TrackCount        int    `json:"trackCount"`
	TrackNumber       int    `json:"trackNumber"`
	DiscNumber        int    `json:"discNumber"`
	ReleaseDate       string `json:"releaseDate"`
	PrimaryGenreName  string `json:"primaryGenreName"`
	Copyright         string `json:"copyright"`
	TrackExplicitness string `json:"trackExplicitness"`
}

func (*ITunesAdapter) ArtworkSource() string { return "itunes" }
