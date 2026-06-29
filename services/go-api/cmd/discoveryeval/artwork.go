package main

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"altune/go-api/internal/discovery/domain"
	discoveryEval "altune/go-api/internal/discovery/service/eval"
	"altune/go-api/internal/shared/config"

	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
)

// runArtworkEval measures artwork-resolution coverage over the library's distinct
// artists: for each it runs the REAL pipeline and buckets the top artist result by
// HOW its image resolved (identity / provider / name / blank). It prints aggregate
// percentages plus the attributed gaps — the name-guesses (risky for same-name
// artists) and the blanks — so coverage is a number to optimize, not an artist you
// hand-tested. Live: bound it with -limit and -concurrency. Flush Redis first for a
// cold measurement (warm artwork-cache hits report as "cache").
func runArtworkEval(
	ctx context.Context,
	cfg *config.Config,
	pool *pgxpool.Pool,
	redisClient *goredis.Client,
	opts options,
) error {
	artists, err := loadLibraryArtists(ctx, pool, opts.limit, opts.random)
	if err != nil {
		return err
	}
	if len(artists) == 0 {
		return fmt.Errorf("no artists found in library")
	}

	searcher, drain := buildEvalSearcher(cfg, pool, redisClient)
	defer drain()

	concurrency := opts.concurrency
	if concurrency < 1 {
		concurrency = 4
	}
	paths := make([]string, len(artists))
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for i, artist := range artists {
		wg.Add(1)
		go func(i int, artist string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			paths[i] = resolveArtistArtworkPath(ctx, searcher, artist)
		}(i, artist)
	}
	wg.Wait()

	newArtworkReport(artists, paths).print()
	return nil
}

// resolveArtistArtworkPath runs the real search for an artist and returns how the
// top artist result's image resolved, or "blank" when no artist surfaced / no image.
func resolveArtistArtworkPath(ctx context.Context, searcher discoveryEval.Searcher, artist string) string {
	results, err := searcher.Search(ctx, artist)
	if err != nil {
		return "blank"
	}
	for i := range results {
		if results[i].Kind != domain.ResultKindArtist {
			continue
		}
		if results[i].ImageURL == "" {
			return "blank"
		}
		if p, ok := results[i].Extras["artwork_path"].(string); ok && p != "" {
			return p
		}
		return "unknown"
	}
	return "blank"
}

type artworkReport struct {
	total  int
	byPath map[string]int
	names  []string // provisional (name-guessed) — risky for same-name artists
	blanks []string // no image
}

func newArtworkReport(artists, paths []string) artworkReport {
	r := artworkReport{total: len(artists), byPath: map[string]int{}}
	for i, p := range paths {
		r.byPath[p]++
		switch p {
		case "name":
			r.names = append(r.names, artists[i])
		case "blank":
			r.blanks = append(r.blanks, artists[i])
		}
	}
	sort.Strings(r.names)
	sort.Strings(r.blanks)
	return r
}

func (r artworkReport) print() {
	pct := func(n int) float64 { return 100 * float64(n) / float64(r.total) }
	identity := r.byPath["identity"] + r.byPath["durable-identity"]
	provider := r.byPath["provider"] + r.byPath["cache"]
	name := r.byPath["name"]
	blank := r.byPath["blank"] + r.byPath["unknown"]

	fmt.Printf("\n# artwork coverage — %d artists\n", r.total)
	fmt.Printf("  identity (id-pinned, trustworthy)        : %4d  %5.1f%%\n", identity, pct(identity))
	fmt.Printf("  provider (entity's own image)            : %4d  %5.1f%%\n", provider, pct(provider))
	fmt.Printf("  name     (provisional — may be wrong)    : %4d  %5.1f%%\n", name, pct(name))
	fmt.Printf("  blank    (no image)                      : %4d  %5.1f%%\n", blank, pct(blank))
	fmt.Printf("  ----------------------------------------\n")
	fmt.Printf("  correct-or-own image (identity+provider) : %4d  %5.1f%%\n", identity+provider, pct(identity+provider))
	fmt.Printf("  at-risk (name) + blank — the gap         : %4d  %5.1f%%\n", name+blank, pct(name+blank))
	if c := r.byPath["cache"]; c > 0 {
		fmt.Printf("  note: %d reported as 'cache' (warm artwork cache) — flush Redis for a cold run\n", c)
	}

	printArtworkList("name-guessed (add an identity source, or accept)", r.names)
	printArtworkList("blank (no image anywhere we currently look)", r.blanks)
}

func printArtworkList(label string, items []string) {
	if len(items) == 0 {
		return
	}
	const maxList = 40
	fmt.Printf("\n  %s — %d:\n", label, len(items))
	for i, it := range items {
		if i >= maxList {
			fmt.Printf("    … and %d more\n", len(items)-maxList)
			break
		}
		fmt.Printf("    - %s\n", it)
	}
}

// loadLibraryArtists returns the library's distinct artists, optionally sampled.
func loadLibraryArtists(ctx context.Context, pool *pgxpool.Pool, limit int, random bool) ([]string, error) {
	query := `SELECT DISTINCT artist FROM tracks WHERE artist <> '' ORDER BY artist`
	if random {
		query = `SELECT artist FROM (SELECT DISTINCT artist FROM tracks WHERE artist <> '') d ORDER BY random()`
	}
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}
	rows, err := pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query artists: %w", err)
	}
	defer rows.Close()
	artists := []string{}
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err != nil {
			return nil, fmt.Errorf("scan artist: %w", err)
		}
		artists = append(artists, a)
	}
	return artists, rows.Err()
}
