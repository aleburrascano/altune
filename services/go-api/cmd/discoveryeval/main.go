// Command discoveryeval runs the offline discovery diagnostics that gate and
// inform the pipeline rebuild (plan 002). It exercises the real search pipeline
// in-process (via app.BuildSearchService) and reads discovery's own telemetry;
// it is meant to run nightly / on demand, NOT per-commit.
//
// Modes (-mode):
//   - eval     : library-derived "artist title → #1" quality-regression report.
//   - signal-a : zero-result / abandoned-search coverage-gap report.
//
// Telemetry emission is disabled for eval searches (nil event store) so
// synthetic searches never pollute the telemetry the signals read.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"altune/go-api/internal/app"
	discoveryPersistence "altune/go-api/internal/discovery/adapters/persistence"
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

type options struct {
	mode        string
	limit       int
	concurrency int
	sinceDays   int
	top         int
	topK        int
	jsonPath    string
	random      bool
}

func main() {
	var opts options
	flag.StringVar(&opts.mode, "mode", "eval", "eval | signal-a | signal-b")
	flag.IntVar(&opts.limit, "limit", 0, "eval: max entities to evaluate (0 = all)")
	flag.IntVar(&opts.concurrency, "concurrency", 4, "eval: parallel searches against live providers")
	flag.IntVar(&opts.sinceDays, "since-days", 30, "signals: telemetry window in days")
	flag.IntVar(&opts.top, "top", 50, "signals: max ranked entries")
	flag.IntVar(&opts.topK, "top-k", 3, "eval: top-K window — entity passes if it ranks within the top K (1 = strict #1)")
	flag.StringVar(&opts.jsonPath, "json", "", "write the full JSON report to this path (default: stdout summary only)")
	flag.BoolVar(&opts.random, "random", false, "eval: sample entities randomly instead of alphabetically (use with -limit for a representative sample)")
	flag.Parse()

	if err := run(opts); err != nil {
		fmt.Fprintf(os.Stderr, "discoveryeval: %v\n", err)
		os.Exit(1)
	}
}

func run(opts options) error {
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

	switch opts.mode {
	case "eval":
		return runEval(ctx, cfg, pool, redisClient, opts)
	case "signal-a":
		return runSignalA(ctx, pool, redisClient, opts)
	case "signal-b":
		return runSignalB(ctx, cfg, pool, opts)
	default:
		return fmt.Errorf("unknown mode %q (want eval | signal-a | signal-b)", opts.mode)
	}
}

// ---- eval mode ----------------------------------------------------------

func runEval(ctx context.Context, cfg *config.Config, pool *pgxpool.Pool, redisClient *goredis.Client, opts options) error {
	entities, err := loadLibraryEntities(ctx, pool, opts.limit, opts.random)
	if err != nil {
		return fmt.Errorf("load library: %w", err)
	}
	fmt.Fprintf(os.Stderr, "evaluating %d unique entities (concurrency=%d, top-%d)...\n", len(entities), opts.concurrency, opts.topK)

	progress := func(done, total int) {
		fmt.Fprintf(os.Stderr, "\r  %d/%d (%d%%)", done, total, done*100/total)
		if done == total {
			fmt.Fprintln(os.Stderr)
		}
	}

	// nil event store: eval searches must not emit telemetry.
	searchSvc := app.BuildSearchService(cfg, pool, redisClient, nil)
	report := discoveryService.RunLibraryEval(ctx, entities, searchAdapter{svc: searchSvc}, opts.concurrency, opts.topK, progress)
	searchSvc.WaitForIngest() // drain any best-effort vocabulary writes before exit

	if err := maybeWriteJSON(opts.jsonPath, report); err != nil {
		return err
	}
	fmt.Print(renderEval(report))
	return nil
}

// loadLibraryEntities reads the distinct (title, artist) pairs across ALL users.
// This is an offline-only cross-context read of the catalog's tracks table; it
// lives in the composition root and never touches the request path.
func loadLibraryEntities(ctx context.Context, pool *pgxpool.Pool, limit int, random bool) ([]discoveryService.LibraryEntity, error) {
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

// ---- signal-a mode ------------------------------------------------------

func runSignalA(ctx context.Context, pool *pgxpool.Pool, redisClient *goredis.Client, opts options) error {
	eventStore := discoveryPersistence.NewPgxEventStore(pool)

	// A correction filter drops misspellings from the strong gaps; without a
	// vocabulary store it is simply skipped (every zero-result counts as a gap).
	var svc *discoveryService.CoverageSignalAService
	if vocab := app.BuildVocabularyStore(redisClient); vocab != nil {
		svc = discoveryService.NewCoverageSignalAService(eventStore, discoveryService.NewCorrectionService(vocab))
	} else {
		svc = discoveryService.NewCoverageSignalAService(eventStore, nil)
	}

	since := time.Now().UTC().AddDate(0, 0, -opts.sinceDays)
	report, err := svc.Execute(ctx, since, opts.top)
	if err != nil {
		return err
	}

	if err := maybeWriteJSON(opts.jsonPath, report); err != nil {
		return err
	}
	fmt.Print(renderSignalA(report, opts.sinceDays))
	return nil
}

// ---- signal-b mode ------------------------------------------------------

func runSignalB(ctx context.Context, cfg *config.Config, pool *pgxpool.Pool, opts options) error {
	artists, err := loadDistinctArtists(ctx, pool, opts.limit)
	if err != nil {
		return fmt.Errorf("load artists: %w", err)
	}
	fmt.Fprintf(os.Stderr, "scanning %d distinct artists across providers (concurrency=%d)...\n", len(artists), opts.concurrency)

	svc := discoveryService.NewCoverageSignalBService(app.BuildConsensusProviders(cfg))
	report, err := svc.Execute(ctx, artists, opts.concurrency)
	if err != nil {
		return err
	}

	if err := maybeWriteJSON(opts.jsonPath, report); err != nil {
		return err
	}
	fmt.Print(renderSignalB(report))
	return nil
}

func loadDistinctArtists(ctx context.Context, pool *pgxpool.Pool, limit int) ([]string, error) {
	query := `SELECT DISTINCT artist FROM tracks WHERE artist <> '' ORDER BY artist`
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
		var artist string
		if err := rows.Scan(&artist); err != nil {
			return nil, fmt.Errorf("scan artist: %w", err)
		}
		artists = append(artists, artist)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate artists: %w", err)
	}
	return artists, nil
}

// ---- output -------------------------------------------------------------

func maybeWriteJSON(path string, report any) error {
	if path == "" {
		return nil
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal json: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write json: %w", err)
	}
	fmt.Fprintf(os.Stderr, "wrote JSON report to %s\n", path)
	return nil
}

func renderEval(report discoveryService.EvalReport) string {
	out := fmt.Sprintf("# Discovery library eval — %s\n\n", time.Now().UTC().Format(time.RFC3339))
	out += fmt.Sprintf("- Total: %d (evaluated %d, skipped %d)\n", report.Total, report.Evaluated, report.Skipped)
	out += fmt.Sprintf("- Top-1: %d (%.1f%%)\n", report.Top1Passed, report.Top1Rate()*100)
	out += fmt.Sprintf("- Top-%d: %d (%.1f%%) — of which %d ranked below #1\n", report.K, report.TopKPassed, report.TopKRate()*100, report.TopKPassed-report.Top1Passed)
	out += fmt.Sprintf("- Failed (not in top-%d): %d\n", report.K, report.Failed)
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

func renderSignalA(report *discoveryService.CoverageReportA, sinceDays int) string {
	out := fmt.Sprintf("# Discovery coverage signal A — %s\n\n", time.Now().UTC().Format(time.RFC3339))
	out += fmt.Sprintf("Window: last %d days. Strong = zero-result (not a typo); weak = results shown but no click.\n\n", sinceDays)

	out += fmt.Sprintf("## Strong gaps — %d (filtered %d typos)\n\n", len(report.Strong), report.FilteredAsTypos)
	out += renderGaps(report.Strong)

	out += fmt.Sprintf("\n## Weak hints — %d\n\n", len(report.Weak))
	out += renderGaps(report.Weak)
	return out
}

func renderSignalB(report *discoveryService.CoverageReportB) string {
	out := fmt.Sprintf("# Discovery coverage signal B — %s\n\n", time.Now().UTC().Format(time.RFC3339))
	out += fmt.Sprintf("Artists scanned: %d. Total entities (union): %d.\n\n", report.ArtistsScanned, report.TotalEntities)

	out += "## Provider imbalance\n\n"
	if len(report.ProviderGaps) == 0 {
		out += "_none_\n"
	}
	for _, g := range report.ProviderGaps {
		out += fmt.Sprintf("- %s: missing %d / %d (%.1f%% gap)\n", g.Provider, g.Missing, g.Union, g.GapPct*100)
	}

	out += "\n## Caveats\n\n"
	for _, c := range report.Caveats {
		out += fmt.Sprintf("- %s\n", c)
	}
	return out
}

func renderGaps(gaps []discoveryService.CoverageGap) string {
	if len(gaps) == 0 {
		return "_none_\n"
	}
	out := ""
	for _, g := range gaps {
		out += fmt.Sprintf("- %q — %d×\n", g.QueryNorm, g.Count)
	}
	return out
}
