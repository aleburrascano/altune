package persistence

import (
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared"
	"context"
	"fmt"

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

func (r *PgxRelationshipQuerier) FindRelatedByAlbum(ctx context.Context, userId shared.UserId, album string, limit int) ([]ports.RelatedTrackMatch, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT DISTINCT ON (lower(title), lower(artist))
			title, artist, album, artwork_url
		FROM tracks
		WHERE user_id = $1 AND lower(album) = lower($2) AND album != ''
		ORDER BY lower(title), lower(artist), added_at DESC
		LIMIT $3`,
		userId.UUID(), album, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("find related by album: %w", err)
	}
	defer rows.Close()
	return scanRelatedMatches(rows)
}

func scanRelatedMatches(rows pgx.Rows) ([]ports.RelatedTrackMatch, error) {
	return collectRows(rows, func(rows pgx.Rows) (ports.RelatedTrackMatch, error) {
		var m ports.RelatedTrackMatch
		if err := rows.Scan(&m.Title, &m.Artist, &m.Album, &m.ArtworkURL); err != nil {
			return m, err
		}
		return m, nil
	})
}
