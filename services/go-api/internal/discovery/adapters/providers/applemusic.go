package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"altune/go-api/internal/discovery/domain"
)

// AppleMusicAdapter searches Apple Music's official Catalog API
// (api.music.apple.com) using the anonymous developer token Apple's own web
// player embeds publicly — see applemusic_token.go. It returns the same
// catalog and the same entity ids as ITunesAdapter's plain Search API, but
// with ISRC, composer credits, and a lyrics-availability flag that API never
// exposed — confirmed by direct comparison (2026-07-22). Because it strictly
// supersedes iTunes Search for search-fan-out purposes, it replaces
// ITunesAdapter's slot in the fan-out (buildDiscoveryProviders) rather than
// running alongside it, avoiding a duplicate call to the identical catalog on
// every search; ITunesAdapter itself is untouched for its other uses (artwork
// chain, album consensus, content lookups).
//
// AIDEV-DECISION: the anonymous devToken is meant for Apple's own web player,
// not third-party use — accepted for self-hosted personal/family use, the
// same risk posture as the SoundCloud/Amazon Music adapters.
type AppleMusicAdapter struct {
	client      *http.Client
	resolver    *appleMusicTokenResolver
	searchURL   string // overridable in tests
	catalogBase string // storefront catalog root, overridable in tests
}

const (
	appleMusicStorefront    = "us"
	appleMusicSearchTimeout = 4 * time.Second
	appleMusicOrigin        = "https://music.apple.com"
	appleMusicUserAgent     = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
	appleMusicArtworkSize   = 500
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
	return map[domain.ResultKind]bool{
		domain.ResultKindTrack:  true,
		domain.ResultKindAlbum:  true,
		domain.ResultKindArtist: true,
	}
}

func (a *AppleMusicAdapter) SearchTimeout() time.Duration { return appleMusicSearchTimeout }

func (a *AppleMusicAdapter) Search(ctx context.Context, query string, kinds map[domain.ResultKind]bool) ([]domain.SearchResult, error) {
	types := appleMusicTypesParam(kinds)
	if types == "" {
		return nil, nil
	}

	token, err := a.resolver.get(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolve apple music token: %w", err)
	}

	results, status, err := a.doSearch(ctx, token, query, types)
	if err != nil && isAuthStatus(status) {
		a.resolver.invalidate()
		token, err = a.resolver.get(ctx)
		if err != nil {
			return nil, fmt.Errorf("re-resolve apple music token: %w", err)
		}
		results, _, err = a.doSearch(ctx, token, query, types)
	}
	return results, err
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

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Origin", appleMusicOrigin)
	req.Header.Set("User-Agent", appleMusicUserAgent)

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, fmt.Errorf("http status %d", resp.StatusCode)
	}

	var body appleMusicSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("decode catalog search response: %w", err)
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
	return results, resp.StatusCode, nil
}

// --- response shapes ---------------------------------------------------
//
// Apple's Catalog API is a clean, flat, officially-documented JSON shape (no
// UI-template tree to walk, unlike Amazon Music) — only the auth is
// unofficial here, not the response format.

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
		ReleaseDate          string            `json:"releaseDate"`
		URL                  string            `json:"url"`
	} `json:"attributes"`
}

type appleMusicAlbum struct {
	ID         string `json:"id"`
	Attributes struct {
		Name        string            `json:"name"`
		ArtistName  string            `json:"artistName"`
		Artwork     appleMusicArtwork `json:"artwork"`
		Copyright   string            `json:"copyright"`
		GenreNames  []string          `json:"genreNames"`
		IsSingle    bool              `json:"isSingle"`
		RecordLabel string            `json:"recordLabel"`
		ReleaseDate string            `json:"releaseDate"`
		TrackCount  int               `json:"trackCount"`
		UPC         string            `json:"upc"`
		URL         string            `json:"url"`
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

// appleMusicArtworkURL fills Apple's "{w}x{h}" artwork URL template with a
// fixed square size.
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
	// record_type: Apple flags singles explicitly; everything else is an album
	// (the Catalog API has no EP/compilation flag). Labelling the common case
	// instead of leaving it blank is what lets the discography bucket it, rather
	// than falling back to whatever another provider happened to say.
	if a.IsSingle {
		extras["record_type"] = "single"
	} else {
		extras["record_type"] = "album"
	}
	if a.UPC != "" {
		extras["upc"] = a.UPC
	}

	r := domain.NewProviderResult(domain.ResultKindAlbum, stripAlbumTypeSuffix(a.Name), a.ArtistName,
		appleMusicArtworkURL(a.Artwork.URL, appleMusicArtworkSize),
		domain.SourceRef{Provider: domain.ProviderAppleMusic, ExternalID: al.ID, URL: a.URL},
		extras)
	r.ReleaseDate = a.ReleaseDate
	r.TrackCount = a.TrackCount
	return r
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

func (*AppleMusicAdapter) ArtworkSource() string { return "applemusic" }
