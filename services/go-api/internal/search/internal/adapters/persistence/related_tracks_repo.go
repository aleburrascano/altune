package persistence

import (
	"altune/go-api/internal/search/internal/ports"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/music"
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresRelationshipQuerier struct {
	pool *pgxpool.Pool
}

const albumCandidateCap = 500

var _ ports.RelationshipQuerier = (*PostgresRelationshipQuerier)(nil)

func NewPostgresRelationshipQuerier(pool *pgxpool.Pool) *PostgresRelationshipQuerier {
	return &PostgresRelationshipQuerier{pool: pool}
}

func (r *PostgresRelationshipQuerier) FindRelatedByAlbumArtist(ctx context.Context, userId ids.UserId, album, artist string, limit int) ([]ports.RelatedTrackMatch, error) {
	candidates, err := r.albumCandidates(ctx, userId, album)
	if err != nil {
		return nil, err
	}
	return keepArtistMatches(candidates, artist, limit), nil
}

func (r *PostgresRelationshipQuerier) albumCandidates(ctx context.Context, userId ids.UserId, album string) ([]albumCandidate, error) {
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
	collected, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (albumCandidate, error) {
		var c albumCandidate
		err := row.Scan(&c.match.Title, &c.match.Artist, &c.match.Album, &c.match.ArtworkURL, &c.albumArtist)
		if err != nil {
			return c, fmt.Errorf("scan related album candidate: %w", err)
		}
		return c, nil
	})
	if err != nil {
		return nil, fmt.Errorf("collect related album candidates: %w", err)
	}
	return collected, nil
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
		if music.PlainNameKey(owner) == "" {
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
	for _, part := range append(music.SplitCredit(credit), credit) {
		if key := music.PlainNameKey(part); key != "" {
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
