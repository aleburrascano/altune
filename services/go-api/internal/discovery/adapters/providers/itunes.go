package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/textnorm"
)

type ITunesAdapter struct {
	client *http.Client
}

func NewITunesAdapter(client *http.Client) *ITunesAdapter {
	return &ITunesAdapter{client: client}
}

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
	u := fmt.Sprintf("https://itunes.apple.com/search?term=%s&entity=%s&limit=15", url.QueryEscape(query), entity)

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("itunes returned %d", resp.StatusCode)
	}

	var body itunesResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
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
	case domain.ResultKindAlbum:
		title = item.CollectionName
		subtitle = item.ArtistName
		if item.TrackCount > 0 {
			extras["track_count"] = item.TrackCount
		}
		if item.ReleaseDate != "" {
			extras["release_date"] = item.ReleaseDate
		}
	case domain.ResultKindArtist:
		title = item.ArtistName
	}

	externalID, sourceURL := itunesSourceRef(item, kind)
	return domain.NewProviderResult(kind, title, subtitle, artworkURL,
		domain.SourceRef{Provider: domain.ProviderITunes, ExternalID: externalID, URL: sourceURL},
		extras)
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

	u := fmt.Sprintf("https://itunes.apple.com/search?term=%s&entity=%s&limit=1", url.QueryEscape(query), entity)
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return "", nil
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return "", nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", nil
	}

	var body itunesResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
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
		"https://itunes.apple.com/lookup?id=%s&entity=%s&limit=50",
		url.QueryEscape(id), entity,
	)
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("itunes lookup returned %d", resp.StatusCode)
	}

	var body itunesResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
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
		"https://itunes.apple.com/search?term=%s&entity=album&limit=5",
		url.QueryEscape(albumTitle),
	)

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return domain.AlbumVerdictUnknown, 0, nil
	}

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
	ReleaseDate       string `json:"releaseDate"`
	PrimaryGenreName  string `json:"primaryGenreName"`
}
