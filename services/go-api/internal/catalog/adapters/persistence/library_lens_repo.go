package persistence

import (
	"context"
	"fmt"
	"strings"
	"time"

	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
)

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

func (r *PgxTrackRepository) ListAlbumsForUser(
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

func (r *PgxTrackRepository) ListArtistsForUser(
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

func (r *PgxTrackRepository) ListFilteredForUser(
	ctx context.Context,
	userId shared.UserId,
	query domain.LibraryQuery,
) ([]*domain.Track, int, error) {
	sql := `SELECT ` + trackColumns + `, COUNT(*) OVER () AS total FROM tracks WHERE user_id = $1`
	args := []any{userId.UUID()}
	if query.Search != "" {
		sql += ` AND (title ILIKE $4 OR artist ILIKE $4 OR album ILIKE $4)`
	}
	sql += trackOrderBy(query.Sort) + ` LIMIT $2 OFFSET $3`
	args = append(args, query.Limit, query.Offset)
	if query.Search != "" {
		args = append(args, likePattern(query.Search))
	}

	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list filtered tracks: %w", err)
	}
	defer rows.Close()

	var tracks []*domain.Track
	total := 0
	for rows.Next() {
		dest, build := trackScanDest()
		dest = append(dest, &total)
		if err := rows.Scan(dest...); err != nil {
			return nil, 0, err
		}
		t, err := build()
		if err != nil {
			return nil, 0, err
		}
		tracks = append(tracks, t)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if err := loadFeaturedForTracks(ctx, r.pool, tracks); err != nil {
		return nil, 0, err
	}
	return tracks, total, nil
}

func (r *PgxTrackRepository) ListOwnedTrackRefs(
	ctx context.Context,
	userId shared.UserId,
) ([]domain.OwnedTrackRef, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, title, artist, acquisition_status, track_number FROM tracks WHERE user_id = $1`,
		userId.UUID(),
	)
	if err != nil {
		return nil, fmt.Errorf("list owned track refs: %w", err)
	}
	defer rows.Close()

	refs := []domain.OwnedTrackRef{}
	for rows.Next() {
		var ref domain.OwnedTrackRef
		if err := rows.Scan(&ref.ID, &ref.Title, &ref.Artist, &ref.AcquisitionStatus, &ref.TrackNumber); err != nil {
			return nil, fmt.Errorf("scan owned track ref: %w", err)
		}
		refs = append(refs, ref)
	}
	return refs, rows.Err()
}
