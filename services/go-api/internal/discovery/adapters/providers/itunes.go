package providers

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

type ITunesAdapter struct {
	client  *http.Client
	limiter *minIntervalLimiter
}

func NewITunesAdapter(client *http.Client) *ITunesAdapter {
	return &ITunesAdapter{
		client:  client,
		limiter: newRateLimiter(itunesEmitInterval, itunesBurst, providerQueueDepth),
	}
}

const (
	itunesEmitInterval = 4 * time.Second
	itunesBurst        = 4
)

const itunesUserAgent = "Altune/1.0 (music manager; self-hosted)"

func (a *ITunesAdapter) SearchTimeout() time.Duration { return 4 * time.Second }

func (a *ITunesAdapter) Name() domain.ProviderName { return domain.ProviderITunes }

func (a *ITunesAdapter) SupportedKinds() map[domain.ResultKind]bool {
	return allSearchKinds()
}

func (a *ITunesAdapter) Search(ctx context.Context, query string, kinds map[domain.ResultKind]bool) ([]domain.SearchResult, error) {
	return searchAcrossKinds(ctx, a.Name().String(), query, kinds, a.SupportedKinds(),
		func(ctx context.Context, kind domain.ResultKind) ([]domain.SearchResult, error) {
			return a.searchKind(ctx, query, kind)
		})
}

func (a *ITunesAdapter) searchKind(ctx context.Context, query string, kind domain.ResultKind) ([]domain.SearchResult, error) {
	entity := itunesEntity(kind)
	u := fmt.Sprintf("https://itunes.apple.com/search?term=%s&entity=%s&country=US&limit=200", url.QueryEscape(query), entity)

	if err := a.limiter.wait(ctx); err != nil {
		return nil, err
	}
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
	case domain.ResultKindArtist:
		title = item.ArtistName
	}

	externalID, sourceURL := itunesSourceRef(item, kind)
	r := domain.NewProviderResult(kind, title, subtitle, artworkURL,
		domain.SourceRef{Provider: domain.ProviderITunes, ExternalID: externalID, URL: sourceURL},
		extras)
	if kind == domain.ResultKindAlbum {
		r.RecordType = iTunesRecordType(item.CollectionName)
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
