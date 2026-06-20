// Command discoveryeval runs the library-derived discovery eval: for every
// unique (title, artist) in the catalog across all users, it searches
// "artist title" and asserts that exact track ranks #1 in the blended view.
//
// It exercises the real search pipeline in-process (via app.BuildSearchService),
// hits live provider APIs, and is meant to run nightly — NOT per-commit. The
// per-commit gate stays the canonical 9-query suite. Telemetry emission is
// disabled (nil event store) so synthetic eval searches never pollute the
// telemetry the coverage signals read.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"altune/go-api/internal/app"
	"altune/go-api/internal/discovery/domain"
	discoveryService "altune/go-api/internal/discovery/service"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/database"
	"altune/go-api/internal/shared/logging"
	sharedRedis "altune/go-api/internal/shared/redis"

	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
)

func main() {
	limit := flag.Int("limit", 0, "max entities to evaluate (0 = all)")
	concurrency := flag.Int("concurrency", 4, "parallel searches against live providers")
	jsonPath := flag.String("json", "", "write the full JSON report to this path (default: stdout summary only)")
	flag.Parse()

	if err := run(*limit, *concurrency, *jsonPath); err != nil {
		fmt.Fprintf(os.Stderr, "discoveryeval: %v\n", err)
		os.Exit(1)
	}
}

func run(limit, concurrency int, jsonPath string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	logging.Setup(cfg)

	ctx := context.Background()

	pool, err := database.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer pool.Close()

	var redisClient *goredis.Client
	if cfg.RedisURL != "" {
		redisClient = sharedRedis.NewClient(ctx, cfg.RedisURL)
		defer redisClient.Close()
	}

	entities, err := loadLibraryEntities(ctx, pool, limit)
	if err != nil {
		return fmt.Errorf("load library: %w", err)
	}
	fmt.Fprintf(os.Stderr, "evaluating %d unique entities (concurrency=%d)...\n", len(entities), concurrency)

	// nil event store: eval searches must not emit telemetry.
	searchSvc := app.BuildSearchService(cfg, pool, redisClient, nil)
	report := discoveryService.RunLibraryEval(ctx, entities, searchAdapter{svc: searchSvc}, concurrency)
	searchSvc.WaitForIngest() // drain any best-effort vocabulary writes before exit

	if jsonPath != "" {
		if err := writeJSON(jsonPath, report); err != nil {
			return fmt.Errorf("write json: %w", err)
		}
		fmt.Fprintf(os.Stderr, "wrote JSON report to %s\n", jsonPath)
	}

	fmt.Print(renderMarkdown(report))
	return nil
}

// loadLibraryEntities reads the distinct (title, artist) pairs across ALL users.
// This is an offline-only cross-context read of the catalog's tracks table; it
// lives in the composition root and never touches the request path.
func loadLibraryEntities(ctx context.Context, pool *pgxpool.Pool, limit int) ([]discoveryService.LibraryEntity, error) {
	query := `SELECT DISTINCT title, artist FROM tracks WHERE artist <> '' ORDER BY artist, title`
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	rows, err := pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query tracks: %w", err)
	}
	defer rows.Close()

	entities := []discoveryService.LibraryEntity{}
	for rows.Next() {
		var title, artist string
		if err := rows.Scan(&title, &artist); err != nil {
			return nil, fmt.Errorf("scan track: %w", err)
		}
		entities = append(entities, discoveryService.LibraryEntity{Title: title, Artist: artist})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tracks: %w", err)
	}
	return entities, nil
}

// searchAdapter wraps the full search service as the eval's narrow Searcher.
// Blended view (all music kinds), no history persistence, synthetic zero user.
type searchAdapter struct {
	svc *discoveryService.SearchMusicService
}

func (a searchAdapter) Search(ctx context.Context, query string) ([]domain.SearchResult, error) {
	kinds := map[domain.ResultKind]bool{
		domain.ResultKindArtist: true,
		domain.ResultKindAlbum:  true,
		domain.ResultKindTrack:  true,
	}
	q, err := domain.NewSearchQuery(query, "", kinds, 20)
	if err != nil {
		return nil, err
	}
	out, err := a.svc.Execute(ctx, shared.UserId{}, q, false)
	if err != nil {
		return nil, err
	}
	return out.Results, nil
}

func writeJSON(path string, report discoveryService.EvalReport) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func renderMarkdown(report discoveryService.EvalReport) string {
	out := fmt.Sprintf("# Discovery library eval — %s\n\n", time.Now().UTC().Format(time.RFC3339))
	out += fmt.Sprintf("- Total: %d (evaluated %d, skipped %d)\n", report.Total, report.Evaluated, report.Skipped)
	out += fmt.Sprintf("- Passed: %d  Failed: %d  Pass rate: %.1f%%\n", report.Passed, report.Failed, report.PassRate()*100)
	if len(report.FailuresByTopKind) > 0 {
		out += "- Failures by top result kind:"
		for kind, n := range report.FailuresByTopKind {
			out += fmt.Sprintf(" %s=%d", kind, n)
		}
		out += "\n"
	}

	out += "\n## Failures\n\n"
	failures := 0
	for _, r := range report.Results {
		if r.Outcome == discoveryService.EvalPass || r.Outcome == discoveryService.EvalSkipped {
			continue
		}
		failures++
		switch r.Outcome {
		case discoveryService.EvalFailNoResults:
			reason := "no results"
			if r.Error != "" {
				reason = "error: " + r.Error
			}
			out += fmt.Sprintf("- %q → %s\n", r.Query, reason)
		case discoveryService.EvalFailWrongTop:
			out += fmt.Sprintf("- %q → #1 was [%s] %q — %s\n", r.Query, r.Top.Kind, r.Top.Title, r.Top.Subtitle)
		}
	}
	if failures == 0 {
		out += "_none_\n"
	}
	return out
}
