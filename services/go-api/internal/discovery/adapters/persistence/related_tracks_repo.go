package persistence

import (
	"context"
	"fmt"

	"altune/go-api/internal/discovery/ports"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PgxRelationshipQuerier struct {
	pool *pgxpool.Pool
}

var _ ports.RelationshipQuerier = (*PgxRelationshipQuerier)(nil)

func NewPgxRelationshipQuerier(pool *pgxpool.Pool) *PgxRelationshipQuerier {
	return &PgxRelationshipQuerier{pool: pool}
}

func (r *PgxRelationshipQuerier) FindRelatedByAlbum(ctx context.Context, album string, limit int) ([]ports.RelatedTrackMatch, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT DISTINCT ON (lower(title), lower(artist))
			title, artist, album, artwork_url
		FROM tracks
		WHERE lower(album) = lower($1) AND album != ''
		ORDER BY lower(title), lower(artist), added_at DESC
		LIMIT $2`,
		album, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("find related by album: %w", err)
	}
	defer rows.Close()
	return scanRelatedMatches(rows)
}

func (r *PgxRelationshipQuerier) FindRelatedByArtist(ctx context.Context, artist string, limit int) ([]ports.RelatedTrackMatch, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT DISTINCT ON (lower(title), lower(artist))
			title, artist, album, artwork_url
		FROM tracks
		WHERE lower(artist) = lower($1)
		ORDER BY lower(title), lower(artist), added_at DESC
		LIMIT $2`,
		artist, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("find related by artist: %w", err)
	}
	defer rows.Close()
	return scanRelatedMatches(rows)
}

func scanRelatedMatches(rows pgx.Rows) ([]ports.RelatedTrackMatch, error) {
	var matches []ports.RelatedTrackMatch
	for rows.Next() {
		var m ports.RelatedTrackMatch
		if err := rows.Scan(&m.Title, &m.Artist, &m.Album, &m.ArtworkURL); err != nil {
			return nil, err
		}
		matches = append(matches, m)
	}
	return matches, rows.Err()
}
