package providers

import (
	"strconv"
	"strings"

	"altune/go-api/internal/discovery/domain"
)

type scSearchResponse struct {
	Collection []scAPITrack `json:"collection"`
	NextHref   string       `json:"next_href"`
}

type scAPITrack struct {
	ID                int64  `json:"id"`
	Kind              string `json:"kind"`
	Title             string `json:"title"`
	PermalinkURL      string `json:"permalink_url"`
	DurationMs        int64  `json:"duration"`
	Genre             string `json:"genre"`
	ArtworkURL        string `json:"artwork_url"`
	PlaybackCount     int64  `json:"playback_count"`
	LikesCount        int64  `json:"likes_count"`
	RepostsCount      int64  `json:"reposts_count"`
	ReleaseDate       string `json:"release_date"`
	DisplayDate       string `json:"display_date"`
	CreatedAt         string `json:"created_at"`
	PublisherMetadata struct {
		ISRC       string `json:"isrc"`
		AlbumTitle string `json:"album_title"`
	} `json:"publisher_metadata"`
	User struct {
		Username string `json:"username"`
	} `json:"user"`
}

func mapSoundCloudAPITrack(t scAPITrack) (domain.SearchResult, bool) {
	if t.ID == 0 || strings.TrimSpace(t.Title) == "" {
		return domain.SearchResult{}, false
	}
	if t.Kind != "" && t.Kind != "track" {
		return domain.SearchResult{}, false
	}

	extras := map[string]any{}
	if t.DurationMs > 0 {
		extras["duration"] = float64(t.DurationMs) / 1000.0
	}
	if t.PlaybackCount > 0 {
		extras["playback_count"] = t.PlaybackCount
	}
	if t.LikesCount > 0 {
		extras["likes_count"] = t.LikesCount
	}
	if t.RepostsCount > 0 {
		extras["reposts_count"] = t.RepostsCount
	}
	if g := strings.TrimSpace(t.Genre); g != "" {
		extras["genre"] = g
	}
	if al := strings.TrimSpace(t.PublisherMetadata.AlbumTitle); al != "" {
		extras["album"] = al
	}

	r := domain.NewProviderResult(domain.ResultKindTrack, t.Title, t.User.Username, upgradeArtworkResolution(t.ArtworkURL),
		domain.SourceRef{Provider: domain.ProviderSoundCloud, ExternalID: strconv.FormatInt(t.ID, 10), URL: t.PermalinkURL},
		extras)
	r.ISRC = strings.TrimSpace(t.PublisherMetadata.ISRC)
	r.Album = strings.TrimSpace(t.PublisherMetadata.AlbumTitle)
	r.ReleaseDate = scBestReleaseDate(t.ReleaseDate, t.DisplayDate, t.CreatedAt)
	if t.DurationMs > 0 {
		r.Duration = int(t.DurationMs / 1000)
	}
	return r, true
}

type scAPIAlbum struct {
	ID           int64  `json:"id"`
	Kind         string `json:"kind"`
	Title        string `json:"title"`
	PermalinkURL string `json:"permalink_url"`
	ArtworkURL   string `json:"artwork_url"`
	SetType      string `json:"set_type"`
	Genre        string `json:"genre"`
	TrackCount   int    `json:"track_count"`
	ReleaseDate  string `json:"release_date"`
	DisplayDate  string `json:"display_date"`
	CreatedAt    string `json:"created_at"`
	User         struct {
		Username string `json:"username"`
	} `json:"user"`
	Tracks []scPlaylistTrackRef `json:"tracks"`
}

type scPlaylistTrackRef struct {
	ID int64 `json:"id"`
}

func scBestReleaseDate(releaseDate, displayDate, createdAt string) string {
	for _, d := range []string{releaseDate, displayDate, createdAt} {
		if s := strings.TrimSpace(d); s != "" {
			return s
		}
	}
	return ""
}

func mapSoundCloudAPIAlbum(a scAPIAlbum) (domain.SearchResult, bool) {
	if a.ID == 0 || strings.TrimSpace(a.Title) == "" {
		return domain.SearchResult{}, false
	}

	extras := map[string]any{}
	if st := strings.TrimSpace(a.SetType); st != "" {
		extras["record_type"] = st
	}
	if g := strings.TrimSpace(a.Genre); g != "" {
		extras["genre"] = g
	}

	r := domain.NewProviderResult(domain.ResultKindAlbum, a.Title, a.User.Username, upgradeArtworkResolution(a.ArtworkURL),
		domain.SourceRef{Provider: domain.ProviderSoundCloud, ExternalID: strconv.FormatInt(a.ID, 10), URL: a.PermalinkURL},
		extras)
	r.TrackCount = a.TrackCount
	r.ReleaseDate = scBestReleaseDate(a.ReleaseDate, a.DisplayDate, a.CreatedAt)
	return r, true
}

func mapSoundCloudStandaloneSingle(t scAPITrack) (domain.SearchResult, bool) {
	if t.ID == 0 || strings.TrimSpace(t.Title) == "" {
		return domain.SearchResult{}, false
	}
	if t.Kind != "" && t.Kind != "track" {
		return domain.SearchResult{}, false
	}
	extras := map[string]any{"record_type": "single"}
	if g := strings.TrimSpace(t.Genre); g != "" {
		extras["genre"] = g
	}
	r := domain.NewProviderResult(domain.ResultKindAlbum, t.Title, t.User.Username, upgradeArtworkResolution(t.ArtworkURL),
		domain.SourceRef{Provider: domain.ProviderSoundCloud, ExternalID: strconv.FormatInt(t.ID, 10), URL: t.PermalinkURL},
		extras)
	r.TrackCount = 1
	r.ReleaseDate = scBestReleaseDate(t.ReleaseDate, t.DisplayDate, t.CreatedAt)
	return r, true
}

type scAPIUser struct {
	ID           int64  `json:"id"`
	Kind         string `json:"kind"`
	Username     string `json:"username"`
	PermalinkURL string `json:"permalink_url"`
	AvatarURL    string `json:"avatar_url"`
}

func mapSoundCloudAPIUser(u scAPIUser) (domain.SearchResult, bool) {
	if u.ID == 0 || strings.TrimSpace(u.Username) == "" {
		return domain.SearchResult{}, false
	}

	return domain.NewProviderResult(domain.ResultKindArtist, u.Username, "", upgradeArtworkResolution(u.AvatarURL),
		domain.SourceRef{Provider: domain.ProviderSoundCloud, ExternalID: strconv.FormatInt(u.ID, 10), URL: u.PermalinkURL},
		nil), true
}

func upgradeArtworkResolution(artworkURL string) string {
	if artworkURL == "" {
		return ""
	}
	return strings.Replace(artworkURL, "-large.", "-t500x500.", 1)
}
