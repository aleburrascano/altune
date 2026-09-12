package persistence

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"

	"github.com/jackc/pgx/v5/pgxpool"
)

type PgxLibraryLensRepository struct {
	pool *pgxpool.Pool
}

func NewPgxLibraryLensRepository(pool *pgxpool.Pool) *PgxLibraryLensRepository {
	return &PgxLibraryLensRepository{pool: pool}
}

const albumGroupKey = `lower(t.album) || '|||' || lower(coalesce(t.album_artist, t.artist))`
const artistGroupKey = `lower(t.artist)`

const albumSelect = `
	SELECT ` + albumGroupKey + ` AS group_key,
		min(t.album) AS album,
		min(coalesce(t.album_artist, t.artist)) AS artist,
		(array_agg(t.artwork_url ORDER BY t.added_at DESC) FILTER (WHERE t.artwork_url IS NOT NULL))[1] AS artwork_url,
		(array_agg(t.year ORDER BY t.added_at DESC) FILTER (WHERE t.year IS NOT NULL))[1] AS year,
		count(*) AS track_count,
		max(t.added_at) AS most_recent_added_at
	FROM tracks t
	WHERE t.user_id = $1 AND t.album IS NOT NULL AND t.album <> ''`

const artistSelect = `
	SELECT ` + artistGroupKey + ` AS group_key,
		min(t.artist) AS artist,
		(array_agg(t.artwork_url ORDER BY t.added_at DESC) FILTER (WHERE t.artwork_url IS NOT NULL))[1] AS artwork_url,
		count(*) AS track_count,
		max(t.added_at) AS most_recent_added_at
	FROM tracks t
	WHERE t.user_id = $1`

func likePattern(search string) string {
	escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(search)
	return "%" + escaped + "%"
}

func limitOffsetClause(query domain.LibraryQuery, args *[]any) string {
	clause := ""
	if query.Limit > 0 {
		*args = append(*args, query.Limit)
		clause += ` LIMIT $` + strconv.Itoa(len(*args))
	}
	if query.Offset > 0 {
		*args = append(*args, query.Offset)
		clause += ` OFFSET $` + strconv.Itoa(len(*args))
	}
	return clause
}

func albumOrderBy(sort domain.LibrarySort) string {
	switch sort {
	case domain.SortAlphabetical:
		return ` ORDER BY min(t.album) ASC`
	case domain.SortYear:
		return ` ORDER BY (array_agg(t.year ORDER BY t.added_at DESC) FILTER (WHERE t.year IS NOT NULL))[1] DESC NULLS LAST`
	default:
		return ` ORDER BY max(t.added_at) DESC`
	}
}

func artistOrderBy(sort domain.LibrarySort) string {
	if sort == domain.SortAlphabetical {
		return ` ORDER BY min(t.artist) ASC`
	}
	return ` ORDER BY max(t.added_at) DESC`
}

func (r *PgxLibraryLensRepository) ListAlbumsForUser(
	ctx context.Context,
	userId shared.UserId,
	query domain.LibraryQuery,
) ([]domain.AlbumGroup, error) {
	sql := albumSelect
	args := []any{userId.UUID()}
	if query.Search != "" {
		sql += ` AND (t.album ILIKE $2 OR t.artist ILIKE $2 OR t.album_artist ILIKE $2)`
		args = append(args, likePattern(query.Search))
	}
	sql += ` GROUP BY ` + albumGroupKey + albumOrderBy(query.Sort)
	sql += limitOffsetClause(query, &args)

	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("list library albums: %w", err)
	}
	defer rows.Close()

	albums := []domain.AlbumGroup{}
	for rows.Next() {
		var g domain.AlbumGroup
		var addedAt time.Time
		if err := rows.Scan(&g.Key, &g.Album, &g.Artist, &g.ArtworkURL, &g.Year, &g.TrackCount, &addedAt); err != nil {
			return nil, fmt.Errorf("scan library album: %w", err)
		}
		g.MostRecentAddedAt = addedAt
		albums = append(albums, g)
	}
	return albums, rows.Err()
}

func (r *PgxLibraryLensRepository) ListArtistsForUser(
	ctx context.Context,
	userId shared.UserId,
	query domain.LibraryQuery,
) ([]domain.ArtistGroup, error) {
	sql := artistSelect
	args := []any{userId.UUID()}
	if query.Search != "" {
		sql += ` AND t.artist ILIKE $2`
		args = append(args, likePattern(query.Search))
	}
	sql += ` GROUP BY ` + artistGroupKey + artistOrderBy(query.Sort)
	sql += limitOffsetClause(query, &args)

	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("list library artists: %w", err)
	}
	defer rows.Close()

	artists := []domain.ArtistGroup{}
	for rows.Next() {
		var g domain.ArtistGroup
		var addedAt time.Time
		if err := rows.Scan(&g.Key, &g.Artist, &g.ArtworkURL, &g.TrackCount, &addedAt); err != nil {
			return nil, fmt.Errorf("scan library artist: %w", err)
		}
		g.MostRecentAddedAt = addedAt
		artists = append(artists, g)
	}
	return artists, rows.Err()
}

func trackOrderBy(sort domain.LibrarySort) string {
	switch sort {
	case domain.SortAlphabetical:
		return ` ORDER BY lower(title) ASC, id DESC`
	case domain.SortYear:
		return ` ORDER BY year DESC NULLS LAST, id DESC`
	default:
		return ` ORDER BY added_at DESC, id DESC`
	}
}

func (r *PgxLibraryLensRepository) ListFilteredForUser(
	ctx context.Context,
	userId shared.UserId,
	query domain.LibraryQuery,
) ([]*domain.Track, int, error) {
	var args []any
	placeholder := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}

	sql := `SELECT ` + trackColumns + `, COUNT(*) OVER () AS total FROM tracks WHERE user_id = ` + placeholder(userId.UUID())
	if query.Search != "" {
		p := placeholder(likePattern(query.Search))
		sql += ` AND (title ILIKE ` + p + ` OR artist ILIKE ` + p + ` OR album ILIKE ` + p + `)`
	}
	sql += trackOrderBy(query.Sort)
	sql += ` LIMIT ` + placeholder(query.Limit) + ` OFFSET ` + placeholder(query.Offset)

	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list filtered tracks: %w", err)
	}
	defer rows.Close()

	tracks, total, err := collectTracksWithTotal(rows)
	if err != nil {
		return nil, 0, err
	}
	if err := loadFeaturedForTracks(ctx, r.pool, tracks); err != nil {
		return nil, 0, err
	}
	return tracks, total, nil
}
