package cache

import (
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/internal/ports"
	"context"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

var _ ports.UnreleasedCache = (*RedisUnreleasedCache)(nil)

type RedisUnreleasedCache struct {
	redisJSON
}

func NewRedisUnreleasedCache(client *goredis.Client, opts ...Option) *RedisUnreleasedCache {
	return &RedisUnreleasedCache{newRedisJSON(client, opts)}
}

func (c *RedisUnreleasedCache) Get(ctx context.Context, provider domain.ProviderName, externalID, nameKey string) ([]domain.UnreleasedSong, bool) {
	if c.disabled() {
		return nil, false
	}
	cached, ok := getJSON[[]cachedSong](ctx, c.redisJSON, unreleasedKey(provider, externalID, nameKey))
	if !ok {
		return nil, false
	}
	if cached == nil {
		return nil, true
	}
	songs := make([]domain.UnreleasedSong, len(cached))
	for i, s := range cached {
		songs[i] = s.song()
	}
	return songs, true
}

func (c *RedisUnreleasedCache) Set(ctx context.Context, provider domain.ProviderName, externalID, nameKey string, songs []domain.UnreleasedSong, ttl time.Duration) error {
	if c.disabled() {
		return nil
	}
	var cached []cachedSong
	if songs != nil {
		cached = make([]cachedSong, len(songs))
		for i, s := range songs {
			cached[i] = cachedFromSong(s)
		}
	}
	return c.setJSON(ctx, unreleasedKey(provider, externalID, nameKey), cached, ttl)
}

const unreleasedKeyVersion = "v9"

func unreleasedKey(provider domain.ProviderName, externalID, nameKey string) string {
	return fmt.Sprintf("unreleased:songs:%s:%s:%s:%s", unreleasedKeyVersion, provider.Key(), externalID, nameKey)
}

type cachedLink struct {
	Provider   string `json:"provider"`
	ExternalID string `json:"external_id"`
	URL        string `json:"url"`
}

type cachedVersion struct {
	Title      string       `json:"title"`
	Label      string       `json:"label,omitempty"`
	Quality    string       `json:"quality,omitempty"`
	AudioLinks []cachedLink `json:"audio_links,omitempty"`
	OpenURL    string       `json:"open_url,omitempty"`
	OpenHost   string       `json:"open_host,omitempty"`
}

type cachedSong struct {
	GeniusID     int          `json:"genius_id"`
	Title        string       `json:"title"`
	CoverURL     string       `json:"cover_url,omitempty"`
	GeniusURL    string       `json:"genius_url"`
	ReleaseDate  string       `json:"release_date,omitempty"`
	HasMedia     *bool        `json:"has_media"`
	Quality      string       `json:"quality,omitempty"`
	TrackerKey   string       `json:"tracker_key,omitempty"`
	AudioLinks   []cachedLink `json:"audio_links,omitempty"`
	OffStreaming bool         `json:"off_streaming,omitempty"`
	OpenURL      string       `json:"open_url,omitempty"`
	OpenHost     string       `json:"open_host,omitempty"`

	Versions []cachedVersion `json:"versions,omitempty"`
}

func cachedLinksOf(links []domain.AudioLink) []cachedLink {
	if links == nil {
		return nil
	}
	out := make([]cachedLink, len(links))
	for i, l := range links {
		out[i] = cachedLink{Provider: string(l.Provider), ExternalID: l.ExternalID, URL: l.URL}
	}
	return out
}

func cachedAudioLinks(links []cachedLink) []domain.AudioLink {
	if links == nil {
		return nil
	}
	out := make([]domain.AudioLink, len(links))
	for i, l := range links {
		out[i] = domain.AudioLink{Provider: domain.AudioProvider(l.Provider), ExternalID: l.ExternalID, URL: l.URL}
	}
	return out
}

func cachedFromSong(s domain.UnreleasedSong) cachedSong {
	out := cachedSong{
		GeniusID: s.GeniusID, Title: s.Title, CoverURL: s.CoverURL, GeniusURL: s.GeniusURL, ReleaseDate: s.ReleaseDate,
		HasMedia: s.HasMedia, Quality: s.Quality, TrackerKey: s.TrackerKey, AudioLinks: cachedLinksOf(s.AudioLinks),
		OffStreaming: s.OffStreaming, OpenURL: s.OpenURL, OpenHost: s.OpenHost,
	}
	if s.Versions != nil {
		out.Versions = make([]cachedVersion, len(s.Versions))
		for i, v := range s.Versions {
			out.Versions[i] = cachedVersion{Title: v.Title, Label: v.Label, Quality: v.Quality, AudioLinks: cachedLinksOf(v.AudioLinks), OpenURL: v.OpenURL, OpenHost: v.OpenHost}
		}
	}
	return out
}

func (s cachedSong) song() domain.UnreleasedSong {
	out := domain.UnreleasedSong{
		GeniusID: s.GeniusID, Title: s.Title, CoverURL: s.CoverURL, GeniusURL: s.GeniusURL, ReleaseDate: s.ReleaseDate,
		HasMedia: s.HasMedia, Quality: s.Quality, TrackerKey: s.TrackerKey, AudioLinks: cachedAudioLinks(s.AudioLinks),
		OffStreaming: s.OffStreaming, OpenURL: s.OpenURL, OpenHost: s.OpenHost,
	}
	if s.Versions != nil {
		out.Versions = make([]domain.UnreleasedVersion, len(s.Versions))
		for i, v := range s.Versions {
			out.Versions[i] = domain.UnreleasedVersion{Title: v.Title, Label: v.Label, Quality: v.Quality, AudioLinks: cachedAudioLinks(v.AudioLinks), OpenURL: v.OpenURL, OpenHost: v.OpenHost}
		}
	}
	return out
}
