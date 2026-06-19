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
	var results []domain.SearchResult

	for kind := range kinds {
		if !a.SupportedKinds()[kind] {
			continue
		}

		items, err := a.searchKind(ctx, query, kind)
		if err != nil {
			slog.WarnContext(ctx, "itunes.search_kind_failed",
				"kind", kind.String(), "query", query, "error", err)
			continue
		}
		results = append(results, items...)
	}

	return results, nil
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
	artworkURL := upscaleArtwork(item.ArtworkURL100, 600)

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
	case domain.ResultKindArtist:
		title = item.ArtistName
	}

	return domain.SearchResult{
		Kind:       kind,
		Title:      title,
		Subtitle:   subtitle,
		ImageURL:   artworkURL,
		Confidence: domain.ConfidenceLow,
		Sources: []domain.SourceRef{{
			Provider:   domain.ProviderITunes,
			ExternalID: fmt.Sprintf("%d", item.TrackID),
			URL:        item.TrackViewURL,
		}},
		Extras: extras,
	}
}

func upscaleArtwork(url string, size int) string {
	return strings.Replace(url, "100x100", fmt.Sprintf("%dx%d", size, size), 1)
}

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
		art := upscaleArtwork(item.ArtworkURL100, 600)
		if art != "" {
			return art, nil
		}
	}
	return "", nil
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
	TrackID          int64  `json:"trackId"`
	TrackName        string `json:"trackName"`
	ArtistID         int64  `json:"artistId"`
	ArtistName       string `json:"artistName"`
	CollectionName   string `json:"collectionName"`
	TrackViewURL     string `json:"trackViewUrl"`
	ArtworkURL100    string `json:"artworkUrl100"`
	PreviewURL       string `json:"previewUrl"`
	TrackTimeMillis  int64  `json:"trackTimeMillis"`
	PrimaryGenreName string `json:"primaryGenreName"`
}
