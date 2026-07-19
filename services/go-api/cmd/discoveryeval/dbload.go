package main

import (
	"context"
	"fmt"

	discoveryEval "altune/go-api/internal/discovery/service/eval"

	"github.com/jackc/pgx/v5/pgxpool"
)

// loadLibraryEntities reads the distinct (title, artist) pairs across ALL users.
// This is an offline-only cross-context read of the catalog's tracks table; it
// lives in the composition root and never touches the request path.
func loadLibraryEntities(ctx context.Context, pool *pgxpool.Pool, limit int, random bool) ([]discoveryEval.LibraryEntity, error) {
	// Random sampling needs a subquery: DISTINCT must resolve before ORDER BY random().
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
	defer rows.Close()

	entities := []discoveryEval.LibraryEntity{}
	for rows.Next() {
		var title, artist string
		if err := rows.Scan(&title, &artist); err != nil {
			return nil, fmt.Errorf("scan track: %w", err)
		}
		entities = append(entities, discoveryEval.LibraryEntity{Title: title, Artist: artist})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tracks: %w", err)
	}
	return entities, nil
}

// loadLibraryTerms reads the distinct artist and title strings across all users
// — the known-good vocabulary the correction harness perturbs.
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

// queryStrings runs a single-column string query against the tracks table,
// optionally capped with LIMIT, and scans every row — the shape shared by the
// harnesses' single-column loaders (loadLibraryTerms, loadDistinctArtists).
func queryStrings(ctx context.Context, pool *pgxpool.Pool, query string, limit int) ([]string, error) {
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	rows, err := pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	out := []string{}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate: %w", err)
	}
	return out, nil
}
