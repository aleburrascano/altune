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
	tat    time.Time
}

func NewITunesAdapter(client *http.Client) *ITunesAdapter {
	return &ITunesAdapter{client: client}
}

const (
	itunesEmitInterval = 4 * time.Second
	itunesBurst        = 4
)

const itunesUserAgent = "Altune/1.0 (music manager; self-hosted)"

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
		r.ReleaseDate = item.ReleaseDate
		if item.TrackTimeMillis > 0 {
			r.Duration = int(item.TrackTimeMillis / 1000)
		}
	}
	return r
}

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

const iTunesListArtworkSize = 600

const iTunesHeroArtworkSize = 1500

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

func (a *ITunesAdapter) GetAlbumTracks(ctx context.Context, _ domain.ProviderName, externalID string) ([]domain.SearchResult, error) {
	return a.lookupContent(ctx, externalID, "song")
}

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

func itunesContentTarget(entity string) (wrapperType string, kind domain.ResultKind) {
	if entity == "album" {
		return "collection", domain.ResultKindAlbum
	}
	return "track", domain.ResultKindTrack
}

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
	lower := strings.ToLower(name)
	if len(lower) != len(name) {
		return name
	}
	for _, suffix := range itunesTypeSuffixes {
		if strings.HasSuffix(lower, strings.ToLower(suffix)) {
			return strings.TrimSpace(name[:len(name)-len(suffix)])
		}
	}
	return name
}

func stripAlbumTypeSuffix(title string) string {
	for _, suffix := range []string{" - Single", " - EP"} {
		if len(title) >= len(suffix) && strings.EqualFold(title[len(title)-len(suffix):], suffix) {
			return strings.TrimSpace(title[:len(title)-len(suffix)])
		}
	}
	return title
}

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
