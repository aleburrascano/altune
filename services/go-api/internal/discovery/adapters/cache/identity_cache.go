package cache

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"altune/go-api/internal/discovery/domain"

	goredis "github.com/redis/go-redis/v9"
)

const (
	identityPositiveTTL = 30 * 24 * time.Hour // confirmed/contamination
	identityUnknownTTL  = 24 * time.Hour      // suspect/unknown
	profileWithMBIDTTL  = 7 * 24 * time.Hour  // profile with MBID resolved
	profileNoMBIDTTL    = 24 * time.Hour      // profile without MBID (retry sooner)
)

type identityCacheEntry struct {
	Verdict   string    `json:"verdict"`
	Reason    string    `json:"reason"`
	Layer     string    `json:"layer"`
	FirstSeen time.Time `json:"first_seen"`
}

type IdentityCache struct {
	client *goredis.Client
}

func NewIdentityCache(client *goredis.Client) *IdentityCache {
	return &IdentityCache{client: client}
}

func (c *IdentityCache) Get(ctx context.Context, artistName, albumTitle string) (*identityCacheEntry, bool) {
	if c.client == nil {
		return nil, false
	}
	key := identityCacheKey(artistName, albumTitle)
	val, err := c.client.Get(ctx, key).Result()
	if err != nil {
		return nil, false
	}
	var entry identityCacheEntry
	if err := json.Unmarshal([]byte(val), &entry); err != nil {
		slog.WarnContext(ctx, "identity_cache.unmarshal_failed", "key", key, "error", err)
		return nil, false
	}
	return &entry, true
}

func (c *IdentityCache) Set(ctx context.Context, artistName, albumTitle string, entry identityCacheEntry) {
	if c.client == nil {
		return
	}
	key := identityCacheKey(artistName, albumTitle)
	data, err := json.Marshal(entry)
	if err != nil {
		slog.WarnContext(ctx, "identity_cache.marshal_failed", "key", key, "error", err)
		return
	}
	ttl := identityTTL(entry.Verdict)
	if err := c.client.Set(ctx, key, data, ttl).Err(); err != nil {
		slog.WarnContext(ctx, "identity_cache.set_failed", "key", key, "error", err)
	}
}

// GetVerdict retrieves a cached identity verdict, converting the stored string
// back to a domain.AlbumVerdict. Satisfies the identityCache interface in the
// IdentityResolverService.
func (c *IdentityCache) GetVerdict(ctx context.Context, artistName, albumTitle string) (domain.AlbumVerdict, string, string, time.Time, bool) {
	entry, ok := c.Get(ctx, artistName, albumTitle)
	if !ok {
		return domain.AlbumVerdictUnknown, "", "", time.Time{}, false
	}
	verdict, err := domain.ParseAlbumVerdict(entry.Verdict)
	if err != nil {
		slog.WarnContext(ctx, "identity_cache.parse_verdict_failed",
			"verdict", entry.Verdict, "error", err)
		return domain.AlbumVerdictUnknown, "", "", time.Time{}, false
	}
	return verdict, entry.Reason, entry.Layer, entry.FirstSeen, true
}

// SetVerdict stores an identity verdict in the cache. Satisfies the
// identityCache interface in the IdentityResolverService.
func (c *IdentityCache) SetVerdict(ctx context.Context, artistName, albumTitle string, verdict domain.AlbumVerdict, reason, layer string) {
	// Check if an entry already exists to preserve FirstSeen.
	firstSeen := time.Now()
	if existing, ok := c.Get(ctx, artistName, albumTitle); ok && !existing.FirstSeen.IsZero() {
		firstSeen = existing.FirstSeen
	}
	c.Set(ctx, artistName, albumTitle, identityCacheEntry{
		Verdict:   verdict.String(),
		Reason:    reason,
		Layer:     layer,
		FirstSeen: firstSeen,
	})
}

type profileCacheEntry struct {
	MBID                 string          `json:"mbid"`
	BirthYear            int             `json:"birth_year"`
	Area                 string          `json:"area"`
	ArtistType           string          `json:"artist_type"`
	Disambiguation       string          `json:"disambiguation"`
	GenreCluster         map[string]bool `json:"genre_cluster"`
	KnownISRCRegistrants map[string]bool `json:"isrc_registrants"`
	MBConfirmedTitles    map[string]bool `json:"mb_confirmed_titles"`
}

func (c *IdentityCache) GetProfile(ctx context.Context, artistName string) (domain.ArtistIdentityProfile, bool) {
	if c.client == nil {
		return domain.ArtistIdentityProfile{}, false
	}
	key := profileCacheKey(artistName)
	val, err := c.client.Get(ctx, key).Result()
	if err != nil {
		return domain.ArtistIdentityProfile{}, false
	}
	var entry profileCacheEntry
	if err := json.Unmarshal([]byte(val), &entry); err != nil {
		slog.WarnContext(ctx, "identity_cache.profile_unmarshal_failed", "key", key, "error", err)
		return domain.ArtistIdentityProfile{}, false
	}
	profile := domain.ArtistIdentityProfile{
		MBID:                 entry.MBID,
		BirthYear:            entry.BirthYear,
		Area:                 entry.Area,
		ArtistType:           entry.ArtistType,
		Disambiguation:       entry.Disambiguation,
		GenreCluster:         entry.GenreCluster,
		KnownISRCRegistrants: entry.KnownISRCRegistrants,
		MBConfirmedTitles:    entry.MBConfirmedTitles,
	}
	if profile.GenreCluster == nil {
		profile.GenreCluster = map[string]bool{}
	}
	if profile.KnownISRCRegistrants == nil {
		profile.KnownISRCRegistrants = map[string]bool{}
	}
	if profile.MBConfirmedTitles == nil {
		profile.MBConfirmedTitles = map[string]bool{}
	}
	return profile, true
}

func (c *IdentityCache) SetProfile(ctx context.Context, artistName string, profile domain.ArtistIdentityProfile) {
	if c.client == nil {
		return
	}
	key := profileCacheKey(artistName)
	entry := profileCacheEntry{
		MBID:                 profile.MBID,
		BirthYear:            profile.BirthYear,
		Area:                 profile.Area,
		ArtistType:           profile.ArtistType,
		Disambiguation:       profile.Disambiguation,
		GenreCluster:         profile.GenreCluster,
		KnownISRCRegistrants: profile.KnownISRCRegistrants,
		MBConfirmedTitles:    profile.MBConfirmedTitles,
	}
	data, err := json.Marshal(entry)
	if err != nil {
		slog.WarnContext(ctx, "identity_cache.profile_marshal_failed", "key", key, "error", err)
		return
	}
	ttl := profileNoMBIDTTL
	if profile.MBID != "" {
		ttl = profileWithMBIDTTL
	}
	if err := c.client.Set(ctx, key, data, ttl).Err(); err != nil {
		slog.WarnContext(ctx, "identity_cache.profile_set_failed", "key", key, "error", err)
	}
}

func profileCacheKey(artistName string) string {
	h := sha256.Sum256([]byte("profile|" + artistName))
	return fmt.Sprintf("discovery:identity:v1:profile:%x", h[:8])
}

func identityTTL(verdict string) time.Duration {
	switch verdict {
	case "confirmed", "contamination":
		return identityPositiveTTL
	default:
		return identityUnknownTTL
	}
}

func identityCacheKey(artistName, albumTitle string) string {
	h := sha256.Sum256([]byte(artistName + "|" + albumTitle))
	return fmt.Sprintf("discovery:identity:v1:%x", h[:16])
}
