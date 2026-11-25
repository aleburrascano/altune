package main

import (
	"altune/go-api/internal/discovery"
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func loadLibraryEntities(ctx context.Context, pool *pgxpool.Pool, limit int, random bool) ([]discovery.LibraryEntity, error) {
	order := "ORDER BY artist, title"
	query := `SELECT DISTINCT title, artist FROM tracks WHERE artist <> '' ` + order
	if random {
		query = `SELECT title, artist FROM (SELECT DISTINCT title, artist FROM tracks WHERE artist <> '') d ORDER BY random()`
	}
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	rows, err := pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query tracks: %w", err)
	}
	entities, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (discovery.LibraryEntity, error) {
		var e discovery.LibraryEntity
		err := row.Scan(&e.Title, &e.Artist)
		return e, err
	})
	if err != nil {
		return nil, fmt.Errorf("scan tracks: %w", err)
	}
	return entities, nil
}

func loadLibraryTerms(ctx context.Context, pool *pgxpool.Pool, limit int) ([]string, error) {
	query := `SELECT DISTINCT artist AS term FROM tracks WHERE artist <> ''
	          UNION
	          SELECT DISTINCT title AS term FROM tracks WHERE title <> ''`
	terms, err := queryStrings(ctx, pool, query, limit)
	if err != nil {
		return nil, fmt.Errorf("query terms: %w", err)
	}
	return terms, nil
}

func loadDistinctArtists(ctx context.Context, pool *pgxpool.Pool, limit int) ([]string, error) {
	query := `SELECT DISTINCT artist FROM tracks WHERE artist <> '' ORDER BY artist`
	artists, err := queryStrings(ctx, pool, query, limit)
	if err != nil {
		return nil, fmt.Errorf("query artists: %w", err)
	}
	return artists, nil
}

func queryStrings(ctx context.Context, pool *pgxpool.Pool, query string, limit int) ([]string, error) {
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	rows, err := pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("scan: %w", err)
	}
	return out, nil
}
