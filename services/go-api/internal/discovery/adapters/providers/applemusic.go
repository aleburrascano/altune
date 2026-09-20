package providers

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type AppleMusicAdapter struct {
	client      *http.Client
	resolver    *appleMusicTokenResolver
	searchURL   string
	catalogBase string
}

const (
	appleMusicStorefront    = "us"
	appleMusicSearchTimeout = 4 * time.Second
	appleMusicOrigin        = "https://music.apple.com"
	appleMusicUserAgent     = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
	appleMusicArtworkSize   = 1000
)

func NewAppleMusicAdapter(client *http.Client) *AppleMusicAdapter {
	base := fmt.Sprintf("https://api.music.apple.com/v1/catalog/%s", appleMusicStorefront)
	return &AppleMusicAdapter{
		client:      client,
		resolver:    newAppleMusicTokenResolver(client),
		searchURL:   base + "/search",
		catalogBase: base,
	}
}

func (a *AppleMusicAdapter) Name() domain.ProviderName { return domain.ProviderAppleMusic }

func (a *AppleMusicAdapter) SupportedKinds() map[domain.ResultKind]bool {
	return allSearchKinds()
}

func (a *AppleMusicAdapter) SearchTimeout() time.Duration { return appleMusicSearchTimeout }

func (a *AppleMusicAdapter) Search(ctx context.Context, query string, kinds map[domain.ResultKind]bool) ([]domain.SearchResult, error) {
	types := appleMusicTypesParam(kinds)
	if types == "" {
		return nil, nil
	}

	return withAuthRetry(ctx, a.resolver.cachedResolver,
		func(ctx context.Context, token string) ([]domain.SearchResult, int, error) {
			return a.doSearch(ctx, token, query, types)
		})
}

func appleMusicTypesParam(kinds map[domain.ResultKind]bool) string {
	var types []string
	if kinds[domain.ResultKindTrack] {
		types = append(types, "songs")
	}
	if kinds[domain.ResultKindAlbum] {
		types = append(types, "albums")
	}
	if kinds[domain.ResultKindArtist] {
		types = append(types, "artists")
	}
	return strings.Join(types, ",")
}

func (a *AppleMusicAdapter) doSearch(ctx context.Context, token, query, types string) ([]domain.SearchResult, int, error) {
	u := fmt.Sprintf("%s?term=%s&types=%s&limit=25", a.searchURL, url.QueryEscape(query), types)

	var body appleMusicSearchResponse
	status, err := a.getCatalogJSON(ctx, token, u, &body, "catalog search response")
	if err != nil {
		return nil, status, err
	}

	results := make([]domain.SearchResult, 0, len(body.Results.Songs.Data)+len(body.Results.Albums.Data)+len(body.Results.Artists.Data))
	for _, s := range body.Results.Songs.Data {
		results = append(results, mapAppleMusicSong(s))
	}
	for _, al := range body.Results.Albums.Data {
		results = append(results, mapAppleMusicAlbum(al))
	}
	for _, ar := range body.Results.Artists.Data {
		results = append(results, mapAppleMusicArtist(ar))
	}
	return results, status, nil
}

func (a *AppleMusicAdapter) GetAlbumTracks(ctx context.Context, _ domain.ProviderName, externalID string) ([]domain.SearchResult, error) {
	songs, err := withAuthRetry(ctx, a.resolver.cachedResolver,
		func(ctx context.Context, token string) ([]appleMusicSong, int, error) {
			return a.fetchAlbumTracks(ctx, token, externalID)
		})
	if err != nil {
		return nil, err
	}
	out := make([]domain.SearchResult, 0, len(songs))
	for _, s := range songs {
		out = append(out, mapAppleMusicSong(s))
	}
	return out, nil
}

func (a *AppleMusicAdapter) fetchAlbumTracks(ctx context.Context, token, albumID string) ([]appleMusicSong, int, error) {
	u := fmt.Sprintf("%s/albums/%s/tracks?limit=100", a.catalogBase, url.PathEscape(albumID))
	var body appleMusicResultGroup[appleMusicSong]
	status, err := a.getCatalogJSON(ctx, token, u, &body, "album tracks response")
	if err != nil {
		return nil, status, err
	}
	return body.Data, status, nil
}

// getCatalogJSON performs an authenticated Apple Music catalog GET. Decode
// failures (a non-nil error on a 200) are wrapped as "decode <what>: ...".
func (a *AppleMusicAdapter) getCatalogJSON(ctx context.Context, token, u string, dst any, what string) (int, error) {
	status, err := getJSONWithStatus(ctx, a.client, u, dst,
		withHeader("Authorization", "Bearer "+token),
		withHeader("Origin", appleMusicOrigin),
		withHeader("User-Agent", appleMusicUserAgent))
	if err != nil && status == http.StatusOK {
		return status, fmt.Errorf("decode %s: %w", what, err)
	}
	return status, err
}

type appleMusicSearchResponse struct {
	Results struct {
		Songs   appleMusicResultGroup[appleMusicSong]   `json:"songs"`
		Albums  appleMusicResultGroup[appleMusicAlbum]  `json:"albums"`
		Artists appleMusicResultGroup[appleMusicArtist] `json:"artists"`
	} `json:"results"`
}

type appleMusicResultGroup[T any] struct {
	Data []T `json:"data"`
}

type appleMusicArtwork struct {
	URL string `json:"url"`
}

type appleMusicSong struct {
	ID         string `json:"id"`
	Attributes struct {
		Name                 string            `json:"name"`
		ArtistName           string            `json:"artistName"`
		AlbumName            string            `json:"albumName"`
		Artwork              appleMusicArtwork `json:"artwork"`
		ComposerName         string            `json:"composerName"`
		GenreNames           []string          `json:"genreNames"`
		DurationInMillis     int64             `json:"durationInMillis"`
		DiscNumber           int               `json:"discNumber"`
		TrackNumber          int               `json:"trackNumber"`
		HasLyrics            bool              `json:"hasLyrics"`
		IsAppleDigitalMaster bool              `json:"isAppleDigitalMaster"`
		ISRC                 string            `json:"isrc"`
		ContentRating        string            `json:"contentRating"`
		Previews             []struct {
			URL string `json:"url"`
		} `json:"previews"`
		ReleaseDate string `json:"releaseDate"`
		URL         string `json:"url"`
	} `json:"attributes"`
}

type appleMusicAlbum struct {
	ID         string `json:"id"`
	Attributes struct {
		Name          string            `json:"name"`
		ArtistName    string            `json:"artistName"`
		Artwork       appleMusicArtwork `json:"artwork"`
		ContentRating string            `json:"contentRating"`
		Copyright     string            `json:"copyright"`
		GenreNames    []string          `json:"genreNames"`
		IsSingle      bool              `json:"isSingle"`
		RecordLabel   string            `json:"recordLabel"`
		ReleaseDate   string            `json:"releaseDate"`
		TrackCount    int               `json:"trackCount"`
		UPC           string            `json:"upc"`
		URL           string            `json:"url"`
	} `json:"attributes"`
}

type appleMusicArtist struct {
	ID         string `json:"id"`
	Attributes struct {
		Name           string            `json:"name"`
		Artwork        appleMusicArtwork `json:"artwork"`
		GenreNames     []string          `json:"genreNames"`
		EditorialNotes struct {
			Short string `json:"short"`
		} `json:"editorialNotes"`
		URL string `json:"url"`
	} `json:"attributes"`
}

func appleMusicArtworkURL(templateURL string, size int) string {
	if templateURL == "" {
		return ""
	}
	dim := strconv.Itoa(size)
	return strings.NewReplacer("{w}", dim, "{h}", dim).Replace(templateURL)
}

func mapAppleMusicSong(s appleMusicSong) domain.SearchResult {
	a := s.Attributes
	extras := map[string]any{}
	if a.ComposerName != "" {
		extras["composer"] = a.ComposerName
	}
	if len(a.GenreNames) > 0 {
		extras["genre"] = a.GenreNames[0]
	}
	if a.HasLyrics {
		extras["has_lyrics"] = true
	}
	if a.IsAppleDigitalMaster {
		extras["apple_digital_master"] = true
	}
	if a.TrackNumber > 0 {
		extras["track_number"] = a.TrackNumber
	}
	if a.DiscNumber > 0 {
		extras["disc_number"] = a.DiscNumber
	}
	if len(a.Previews) > 0 && a.Previews[0].URL != "" {
		extras["preview_url"] = a.Previews[0].URL
	}
	if a.ContentRating == "explicit" {
		extras["explicit"] = true
	}

	r := domain.NewProviderResult(domain.ResultKindTrack, a.Name, a.ArtistName,
		appleMusicArtworkURL(a.Artwork.URL, appleMusicArtworkSize),
		domain.SourceRef{Provider: domain.ProviderAppleMusic, ExternalID: s.ID, URL: a.URL},
		extras)
	r.ISRC = a.ISRC
	r.Album = a.AlbumName
	r.ReleaseDate = a.ReleaseDate
	if a.DurationInMillis > 0 {
		r.Duration = int(a.DurationInMillis / 1000)
	}
	return r
}

func mapAppleMusicAlbum(al appleMusicAlbum) domain.SearchResult {
	a := al.Attributes
	extras := map[string]any{}
	if len(a.GenreNames) > 0 {
		extras["genre"] = a.GenreNames[0]
	}
	if a.Copyright != "" {
		extras["copyright"] = a.Copyright
	}
	if a.RecordLabel != "" {
		extras["record_label"] = a.RecordLabel
	}
	if a.UPC != "" {
		extras["upc"] = a.UPC
	}
	if a.ContentRating == "explicit" {
		extras["explicit"] = true
	}

	r := domain.NewProviderResult(domain.ResultKindAlbum, stripAlbumTypeSuffix(a.Name), a.ArtistName,
		appleMusicArtworkURL(a.Artwork.URL, appleMusicArtworkSize),
		domain.SourceRef{Provider: domain.ProviderAppleMusic, ExternalID: al.ID, URL: a.URL},
		extras)
	r.RecordType = appleMusicRecordType(a.IsSingle)
	r.ReleaseDate = a.ReleaseDate
	r.TrackCount = a.TrackCount
	r.UPC = a.UPC
	return r
}

func appleMusicRecordType(isSingle bool) domain.RecordType {
	if isSingle {
		return domain.RecordTypeSingle
	}
	return domain.RecordTypeAlbum
}

func mapAppleMusicArtist(ar appleMusicArtist) domain.SearchResult {
	a := ar.Attributes
	extras := map[string]any{}
	if len(a.GenreNames) > 0 {
		extras["genre"] = a.GenreNames[0]
	}
	if a.EditorialNotes.Short != "" {
		extras["bio"] = a.EditorialNotes.Short
	}

	return domain.NewProviderResult(domain.ResultKindArtist, a.Name, "",
		appleMusicArtworkURL(a.Artwork.URL, appleMusicArtworkSize),
		domain.SourceRef{Provider: domain.ProviderAppleMusic, ExternalID: ar.ID, URL: a.URL},
		extras)
}

func (*AppleMusicAdapter) ArtworkSource() domain.ProviderKey { return domain.ProviderKeyAppleMusic }
