package persistence

import (
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/songfacts"
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PgxRelationshipQuerier struct {
	pool *pgxpool.Pool
}

const albumCandidateCap = 500

var _ ports.RelationshipQuerier = (*PgxRelationshipQuerier)(nil)

func NewPgxRelationshipQuerier(pool *pgxpool.Pool) *PgxRelationshipQuerier {
	return &PgxRelationshipQuerier{pool: pool}
}

func (r *PgxRelationshipQuerier) FindRelatedByAlbumArtist(ctx context.Context, userId shared.UserId, album, artist string, limit int) ([]ports.RelatedTrackMatch, error) {
	candidates, err := r.albumCandidates(ctx, userId, album)
	if err != nil {
		return nil, err
	}
	return keepArtistMatches(candidates, artist, limit), nil
}

func (r *PgxRelationshipQuerier) albumCandidates(ctx context.Context, userId shared.UserId, album string) ([]albumCandidate, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT DISTINCT ON (lower(title), lower(artist))
			title, artist, album, artwork_url, COALESCE(album_artist, '')
		FROM tracks
		WHERE user_id = $1 AND lower(album) = lower($2) AND album != ''
		ORDER BY lower(title), lower(artist), added_at DESC
		LIMIT $3`,
		userId.UUID(), album, albumCandidateCap,
	)
	if err != nil {
		return nil, fmt.Errorf("find related by album artist: %w", err)
	}
	defer rows.Close()
	return collectRows(rows, func(rows pgx.Rows) (albumCandidate, error) {
		var c albumCandidate
		err := rows.Scan(&c.match.Title, &c.match.Artist, &c.match.Album, &c.match.ArtworkURL, &c.albumArtist)
		return c, err
	})
}

type albumCandidate struct {
	match       ports.RelatedTrackMatch
	albumArtist string
}

func keepArtistMatches(candidates []albumCandidate, artist string, limit int) []ports.RelatedTrackMatch {
	wanted := creditKeys(artist)
	kept := make([]ports.RelatedTrackMatch, 0, min(limit, len(candidates)))
	for _, c := range candidates {
		if len(kept) >= limit {
			break
		}
		owner := c.albumArtist
		if songfacts.NameKey(owner) == "" {
			owner = c.match.Artist
		}
		if len(wanted) == 0 || creditsOverlap(wanted, creditKeys(owner)) {
			kept = append(kept, c.match)
		}
	}
	return kept
}

func creditKeys(credit string) map[string]bool {
	keys := map[string]bool{}
	for _, part := range append(songfacts.SplitCredit(credit), credit) {
		if key := songfacts.NameKey(part); key != "" {
			keys[key] = true
		}
	}
	return keys
}

func creditsOverlap(a, b map[string]bool) bool {
	for key := range a {
		if b[key] {
			return true
		}
	}
	return false
}
