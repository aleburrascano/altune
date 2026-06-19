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
)

type DeezerAdapter struct {
	client *http.Client
}

func NewDeezerAdapter(client *http.Client) *DeezerAdapter {
	return &DeezerAdapter{client: client}
}

func (a *DeezerAdapter) Name() domain.ProviderName { return domain.ProviderDeezer }

func (a *DeezerAdapter) SupportedKinds() map[domain.ResultKind]bool {
	return map[domain.ResultKind]bool{
		domain.ResultKindTrack:  true,
		domain.ResultKindAlbum:  true,
		domain.ResultKindArtist: true,
	}
}

func (a *DeezerAdapter) Search(ctx context.Context, query string, kinds map[domain.ResultKind]bool) ([]domain.SearchResult, error) {
	var results []domain.SearchResult

	for kind := range kinds {
		if !a.SupportedKinds()[kind] {
			continue
		}

		items, err := a.searchKind(ctx, query, kind)
		if err != nil {
			slog.WarnContext(ctx, "deezer.search_kind_failed",
				"kind", kind.String(), "query", query, "error", err)
			continue
		}
		results = append(results, items...)
	}

	return results, nil
}

func (a *DeezerAdapter) SearchStructured(ctx context.Context, artist, track string, kinds map[domain.ResultKind]bool) ([]domain.SearchResult, error) {
	var results []domain.SearchResult
	for kind := range kinds {
		if !a.SupportedKinds()[kind] {
			continue
		}
		q := deezerStructuredQuery(artist, track, kind)
		items, err := a.searchKind(ctx, q, kind)
		if err != nil {
			continue
		}
		results = append(results, items...)
	}
	return results, nil
}

func deezerStructuredQuery(artist, track string, kind domain.ResultKind) string {
	switch kind {
	case domain.ResultKindTrack:
		return fmt.Sprintf(`artist:"%s" track:"%s"`, artist, track)
	case domain.ResultKindAlbum:
		return fmt.Sprintf(`artist:"%s" album:"%s"`, artist, track)
	case domain.ResultKindArtist:
		return artist
	default:
		return artist + " " + track
	}
}

func (a *DeezerAdapter) searchKind(ctx context.Context, query string, kind domain.ResultKind) ([]domain.SearchResult, error) {
	endpoint := deezerSearchEndpoint(kind)
	if endpoint == "" {
		return nil, fmt.Errorf("unsupported kind")
	}

	u := fmt.Sprintf("https://api.deezer.com/search/%s?q=%s&limit=15&order=RANKING", endpoint, url.QueryEscape(query))

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
		return nil, fmt.Errorf("deezer returned %d", resp.StatusCode)
	}

	var body deezerSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}

	var results []domain.SearchResult
	for _, item := range body.Data {
		results = append(results, mapDeezerResult(item, kind))
	}
	return results, nil
}

func deezerSearchEndpoint(kind domain.ResultKind) string {
	switch kind {
	case domain.ResultKindTrack:
		return "track"
	case domain.ResultKindAlbum:
		return "album"
	case domain.ResultKindArtist:
		return "artist"
	default:
		return ""
	}
}

func mapDeezerResult(item deezerItem, kind domain.ResultKind) domain.SearchResult {
	var title, subtitle, imageURL string
	extras := make(map[string]any)

	switch kind {
	case domain.ResultKindTrack:
		title = item.Title
		if item.Artist != nil {
			subtitle = item.Artist.Name
		}
		if item.Album != nil {
			imageURL = item.Album.CoverBig
			extras["album"] = item.Album.Title
			extras["deezer_album_id"] = fmt.Sprintf("%d", item.Album.ID)
		}
		if item.ISRC != "" {
			extras["isrc"] = item.ISRC
		}
		extras["duration"] = item.Duration
		if item.Preview != "" {
			extras["preview_url"] = item.Preview
		}
		if item.Rank > 0 {
			extras["rank"] = item.Rank
		}
		if item.NbFan > 0 {
			extras["nb_fan"] = item.NbFan
		}
	case domain.ResultKindAlbum:
		title = item.Title
		if item.Artist != nil {
			subtitle = item.Artist.Name
		}
		imageURL = item.CoverBig
		if item.RecordType != "" {
			extras["record_type"] = item.RecordType
		}
		if item.ReleaseDate != "" {
			extras["release_date"] = item.ReleaseDate
		}
		if item.NbTracks > 0 {
			extras["track_count"] = item.NbTracks
		}
		if item.NbFan > 0 {
			extras["nb_fan"] = item.NbFan
		}
	case domain.ResultKindArtist:
		title = item.Name
		imageURL = item.PictureBig
		if item.NbFan > 0 {
			extras["nb_fan"] = item.NbFan
		}
	}

	return domain.SearchResult{
		Kind:       kind,
		Title:      title,
		Subtitle:   subtitle,
		ImageURL:   imageURL,
		Confidence: domain.ConfidenceLow,
		Sources: []domain.SourceRef{{
			Provider:   domain.ProviderDeezer,
			ExternalID: fmt.Sprintf("%d", item.ID),
			URL:        item.Link,
		}},
		Extras: extras,
	}
}

// Deezer API response types

type deezerSearchResponse struct {
	Data []deezerItem `json:"data"`
}

type deezerItem struct {
	ID          int64        `json:"id"`
	Title       string       `json:"title"`
	Name        string       `json:"name"`
	Link        string       `json:"link"`
	Preview     string       `json:"preview"`
	Duration    int          `json:"duration"`
	ISRC        string       `json:"isrc"`
	CoverBig    string       `json:"cover_big"`
	PictureBig  string       `json:"picture_big"`
	Artist      *deezerRef   `json:"artist"`
	Album       *deezerAlbum `json:"album"`
	RecordType  string       `json:"record_type"`
	ReleaseDate string       `json:"release_date"`
	NbTracks    int          `json:"nb_tracks"`
	Rank        int64        `json:"rank"`
	NbFan       int64        `json:"nb_fan"`
}

type deezerRef struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type deezerAlbum struct {
	ID       int64  `json:"id"`
	Title    string `json:"title"`
	CoverBig string `json:"cover_big"`
}

// Resolve implements ArtworkResolver — best-effort cover lookup by search.
func (a *DeezerAdapter) Resolve(ctx context.Context, kind domain.ResultKind, title, subtitle string, mbid string) (string, error) {
	query := title
	if subtitle != "" {
		query = subtitle + " " + title
	}
	endpoint := deezerSearchEndpoint(kind)
	if endpoint == "" {
		endpoint = "track"
	}

	u := fmt.Sprintf("https://api.deezer.com/search/%s?q=%s&limit=1", endpoint, url.QueryEscape(query))
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

	var body deezerSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", nil
	}
	for _, item := range body.Data {
		var img string
		if item.Album != nil && item.Album.CoverBig != "" {
			img = item.Album.CoverBig
		} else if item.CoverBig != "" {
			img = item.CoverBig
		} else if item.PictureBig != "" {
			img = item.PictureBig
		}
		if img != "" && !IsDeezerPlaceholder(img) {
			return img, nil
		}
	}
	return "", nil
}

// --- AlbumContentProvider + ArtistContentProvider ---

func (a *DeezerAdapter) GetAlbumTracks(ctx context.Context, _ domain.ProviderName, externalID string) ([]domain.SearchResult, error) {
	u := fmt.Sprintf("https://api.deezer.com/album/%s/tracks?limit=50", externalID)
	return a.fetchList(ctx, u, func(item deezerItem) domain.SearchResult {
		return mapDeezerResult(item, domain.ResultKindTrack)
	})
}

func (a *DeezerAdapter) GetArtistTopTracks(ctx context.Context, _ domain.ProviderName, externalID string) ([]domain.SearchResult, error) {
	u := fmt.Sprintf("https://api.deezer.com/artist/%s/top?limit=10", externalID)
	return a.fetchList(ctx, u, func(item deezerItem) domain.SearchResult {
		return mapDeezerResult(item, domain.ResultKindTrack)
	})
}

func (a *DeezerAdapter) GetArtistAlbums(ctx context.Context, _ domain.ProviderName, externalID string) ([]domain.SearchResult, error) {
	u := fmt.Sprintf("https://api.deezer.com/artist/%s/albums?limit=100", externalID)
	return a.fetchList(ctx, u, func(item deezerItem) domain.SearchResult {
		return mapDeezerResult(item, domain.ResultKindAlbum)
	})
}

func (a *DeezerAdapter) fetchList(ctx context.Context, u string, mapper func(deezerItem) domain.SearchResult) ([]domain.SearchResult, error) {
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
		return nil, fmt.Errorf("deezer api returned %d", resp.StatusCode)
	}

	var body deezerSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}

	var results []domain.SearchResult
	for _, item := range body.Data {
		results = append(results, mapper(item))
	}
	return results, nil
}

// --- ChartProvider ---

func (a *DeezerAdapter) FetchCharts(ctx context.Context, limit int) ([]domain.VocabularyEntry, error) {
	var entries []domain.VocabularyEntry
	for _, kind := range []string{"tracks", "artists", "albums"} {
		items, err := a.fetchChartKind(ctx, kind, limit)
		if err != nil {
			continue
		}
		entries = append(entries, items...)
	}
	return entries, nil
}

func (a *DeezerAdapter) fetchChartKind(
	ctx context.Context,
	kind string,
	limit int,
) ([]domain.VocabularyEntry, error) {
	u := fmt.Sprintf(
		"https://api.deezer.com/chart/0/%s?limit=%d",
		kind, limit,
	)
	return a.fetchChartEntries(ctx, u, kind)
}

func (a *DeezerAdapter) fetchChartEntries(
	ctx context.Context,
	u string,
	kind string,
) ([]domain.VocabularyEntry, error) {
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
		return nil, fmt.Errorf("deezer chart returned %d", resp.StatusCode)
	}
	var body deezerSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	return mapDeezerChartItems(body.Data, kind), nil
}

func mapDeezerChartItems(
	items []deezerItem,
	kind string,
) []domain.VocabularyEntry {
	entries := make([]domain.VocabularyEntry, 0, len(items))
	for i, item := range items {
		e := deezerChartEntry(item, kind, i)
		if e.Term == "" {
			continue
		}
		entries = append(entries, e)
	}
	return entries
}

func deezerChartEntry(
	item deezerItem,
	kind string,
	position int,
) domain.VocabularyEntry {
	switch kind {
	case "artists":
		return domain.VocabularyEntry{
			Term:       item.Name,
			Kind:       "artist",
			Popularity: popularityOrPosition(item.NbFan, position),
		}
	case "albums":
		return domain.VocabularyEntry{
			Term:       item.Title,
			Kind:       "album",
			Popularity: popularityOrPosition(item.NbFan, position),
		}
	default:
		return domain.VocabularyEntry{
			Term:       item.Title,
			Kind:       "track",
			Popularity: popularityOrPosition(item.Rank, position),
		}
	}
}

func popularityOrPosition(metric int64, position int) int64 {
	if metric > 0 {
		return metric
	}
	return int64(1000 - position)
}

const DeezerPlaceholderImage = "https://e-cdns-images.dzcdn.net/images/artist//500x500-000000-80-0-0.jpg"

func IsDeezerPlaceholder(u string) bool {
	return strings.Contains(u, "/images/artist//") || strings.Contains(u, "d41d8cd98f00b204e9800998ecf8427e")
}
