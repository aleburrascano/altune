// Command discoveryeval runs the offline discovery quality harnesses. It
// exercises the real search pipeline in-process (via app.BuildSearchService) and
// reads discovery's own telemetry; it runs nightly / on demand, NOT per-commit.
//
// Every gated harness shares one spine (harness.go): run → gate the headline
// metrics against cmd/discoveryeval/baselines.json → print the attributed-failure
// slices → exit 2 on regression. Re-baseline explicitly with -update-baselines
// (use -noise-runs 3 to set an empirical margin). See plan
// docs/plans/2026-06-24-001-test-discovery-eval-harness-program-plan.md.
//
// Modes (-mode):
//   - eval       : ranking — library "artist title → top-K" (gated: top1, topk).
//   - merge      : entity resolution — collapse + over-merge (gated).
//   - correction : synthetic-typo precision/recall, offline (gated).
//   - diversity  : reshaping cost differential on the library oracle (gated).
//   - signal-a   : demand-side coverage gaps from telemetry (gated).
//   - signal-b   : cross-provider coverage imbalance (gated).
//   - health     : fill-rate / bridge-hit / latency (report-only, never gated).
//   - consensus  : per-artist detail dump (-query), or corpus completeness
//     (no -query, report-only).
//
// Telemetry emission is disabled for eval searches (nil event store) so
// synthetic searches never pollute the telemetry the signals read.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	"altune/go-api/internal/app"
	discoveryPersistence "altune/go-api/internal/discovery/adapters/persistence"
	"altune/go-api/internal/discovery/adapters/providers"
	"altune/go-api/internal/discovery/domain"
	discoveryService "altune/go-api/internal/discovery/service"
	discoveryEval "altune/go-api/internal/discovery/service/eval"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/database"
	"altune/go-api/internal/shared/logging"
	sharedRedis "altune/go-api/internal/shared/redis"

	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
)

type options struct {
	mode            string
	limit           int
	concurrency     int
	sinceDays       int
	top             int
	topK            int
	jsonPath        string
	random          bool
	query           string
	baselinesPath   string
	updateBaselines bool
	noiseRuns       int
	typos           int
	corpus          string
	fixtures        string
	record          bool
}

func main() {
	var opts options
	flag.StringVar(&opts.mode, "mode", "eval", "eval | merge | correction | diversity | health | signal-a | signal-b | consensus | artwork | artist-intent | corpus-build")
	flag.IntVar(&opts.limit, "limit", 0, "eval: max entities to evaluate (0 = all)")
	flag.IntVar(&opts.concurrency, "concurrency", 4, "eval: parallel searches against live providers")
	flag.IntVar(&opts.sinceDays, "since-days", 30, "signals: telemetry window in days")
	flag.IntVar(&opts.top, "top", 50, "signals: max ranked entries")
	flag.IntVar(&opts.topK, "top-k", 3, "eval: top-K window — entity passes if it ranks within the top K (1 = strict #1)")
	flag.StringVar(&opts.jsonPath, "json", "", "write the full JSON report to this path (default: stdout summary only)")
	flag.BoolVar(&opts.random, "random", false, "eval: sample entities randomly instead of alphabetically (use with -limit for a representative sample)")
	flag.StringVar(&opts.query, "query", "", "diagnostic: run a single query and dump the top results (bypasses the library eval)")
	flag.StringVar(&opts.baselinesPath, "baselines", "cmd/discoveryeval/baselines.json", "path to the committed baselines/thresholds file")
	flag.BoolVar(&opts.updateBaselines, "update-baselines", false, "re-baseline: measure the current value(s) and write them to -baselines (explicit, reviewed)")
	flag.IntVar(&opts.noiseRuns, "noise-runs", 1, "with -update-baselines: run N times and set the margin to the measured spread (use 3)")
	flag.IntVar(&opts.typos, "typos", 3, "correction: synthetic typos generated per known-good term")
	flag.StringVar(&opts.corpus, "corpus", "exact", "eval/diversity corpus: exact (\"artist title\") | hard (single-token titles, title-only query)")
	flag.StringVar(&opts.fixtures, "fixtures", "", "eval: directory of recorded provider fixtures. With -record, write them (live); without, replay them (deterministic, offline w.r.t. providers)")
	flag.BoolVar(&opts.record, "record", false, "eval: with -fixtures, record provider responses to the directory instead of replaying (runs live, sequentially)")
	flag.Parse()

	if err := run(opts); err != nil {
		if errors.Is(err, errRegressed) {
			fmt.Fprintln(os.Stderr, "discoveryeval: REGRESSION")
			os.Exit(2)
		}
		fmt.Fprintf(os.Stderr, "discoveryeval: %v\n", err)
		os.Exit(1)
	}
}

func run(opts options) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	logging.Setup(cfg.LogLevel, cfg.IsDevelopment())

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

	// The single-query search diagnostic owns -query, except in consensus mode
	// where -query names the artist to build consensus for.
	if opts.query != "" && opts.mode != "consensus" {
		return runQuery(ctx, cfg, pool, redisClient, opts)
	}

	switch opts.mode {
	case "eval":
		return runEval(ctx, cfg, pool, redisClient, opts)
	case "merge":
		return runMerge(ctx, cfg, pool, redisClient, opts)
	case "correction":
		return runCorrection(ctx, pool, redisClient, opts)
	case "diversity":
		return runDiversity(ctx, cfg, pool, redisClient, opts)
	case "health":
		return runHealth(ctx, cfg, pool, redisClient, opts)
	case "signal-a":
		return runSignalA(ctx, pool, redisClient, opts)
	case "signal-b":
		return runSignalB(ctx, cfg, pool, opts)
	case "consensus":
		return runConsensus(ctx, cfg, pool, opts)
	case "artwork":
		return runArtworkEval(ctx, cfg, pool, redisClient, opts)
	case "artist-intent":
		return runArtistIntent(ctx, cfg, pool, redisClient, opts)
	case "corpus-build":
		return runCorpusBuild(ctx, pool, opts)
	default:
		return fmt.Errorf("unknown mode %q (want eval | merge | correction | diversity | health | signal-a | signal-b | consensus | artwork | artist-intent | corpus-build)", opts.mode)
	}
}

// ---- health mode (report-only, never gated) -----------------------------

func runHealth(ctx context.Context, cfg *config.Config, pool *pgxpool.Pool, redisClient *goredis.Client, opts options) error {
	entities, err := loadLibraryEntities(ctx, pool, opts.limit, opts.random)
	if err != nil {
		return fmt.Errorf("load library: %w", err)
	}
	progress := func(done, total int) {
		fmt.Fprintf(os.Stderr, "\r  %d/%d (%d%%)", done, total, done*100/total)
		if done == total {
			fmt.Fprintln(os.Stderr)
		}
	}

	fmt.Fprintf(os.Stderr, "health pass over %d entities (concurrency=%d)...\n", len(entities), opts.concurrency)
	searcher, drain := buildEvalSearcher(cfg, pool, redisClient)
	report := discoveryEval.RunHealthEval(ctx, entities, searcher, opts.concurrency, progress)
	drain()

	if err := maybeWriteJSON(opts.jsonPath, report); err != nil {
		return err
	}
	fmt.Print(renderHealth(report))

	// Report-only: record gauges for visibility/history on an explicit update,
	// but NEVER gate them — a health gauge cannot flip the exit code.
	if opts.updateBaselines {
		existing, err := loadBaselines(opts.baselinesPath)
		if err != nil {
			return err
		}
		if existing == nil {
			existing = discoveryEval.Baselines{}
		}
		for k, v := range discoveryEval.BuildBaselines(report.HealthMetrics(), nil) {
			v.Note = "health gauge — report-only, never gated"
			existing[k] = v
		}
		if err := writeBaselines(opts.baselinesPath, existing); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "recorded %d health gauge(s) in %s\n", len(report.HealthMetrics()), opts.baselinesPath)
	}
	return nil
}

// ---- diversity mode -----------------------------------------------------

func runDiversity(ctx context.Context, cfg *config.Config, pool *pgxpool.Pool, redisClient *goredis.Client, opts options) error {
	entities, err := loadLibraryEntities(ctx, pool, opts.limit, opts.random)
	if err != nil {
		return fmt.Errorf("load library: %w", err)
	}

	progress := func(done, total int) {
		fmt.Fprintf(os.Stderr, "\r  %d/%d (%d%%)", done, total, done*100/total)
		if done == total {
			fmt.Fprintln(os.Stderr)
		}
	}

	corpusEnts, mode := corpusEntities(entities, opts.corpus)
	once := func() (discoveryEval.HarnessReport, error) {
		fmt.Fprintf(os.Stderr, "diversity differential over %d entities (corpus=%s, concurrency=%d, top-%d)...\n", len(corpusEnts), opts.corpus, opts.concurrency, opts.topK)
		svc := app.BuildSearchService(cfg, pool, redisClient, nil)
		vs := variantSearchAdapter{svc: svc}
		report := discoveryEval.RunDiversityEvalMode(ctx, corpusEnts, vs, opts.concurrency, opts.topK, mode, progress)
		svc.WaitForBackground()
		return report, nil
	}
	human := func(r discoveryEval.HarnessReport) string { return renderDiversity(r.(discoveryEval.DiversityReport)) }
	return runHarness("diversity", once, human, opts)
}

// corpusEntities applies the corpus selection. Hard mode keeps only single-token
// titles (the ambiguous case — "Humble", "Scorpion") and signals title-only
// querying; exact mode keeps every entity and queries "artist title".
func corpusEntities(entities []discoveryEval.LibraryEntity, corpus string) ([]discoveryEval.LibraryEntity, discoveryEval.QueryMode) {
	if corpus != "hard" {
		return entities, discoveryEval.QueryExact
	}
	out := []discoveryEval.LibraryEntity{}
	for _, e := range entities {
		if discoveryEval.TokenCount(e.Title) == 1 {
			out = append(out, e)
		}
	}
	return out, discoveryEval.QueryTitleOnly
}

// variantSearchAdapter exposes the pipeline's with/without-reshape seam as the
// diversity harness's VariantSearcher.
type variantSearchAdapter struct {
	svc *discoveryService.Service
}

func (a variantSearchAdapter) SearchVariants(ctx context.Context, query string) ([]domain.SearchResult, []domain.SearchResult) {
	q, err := evalQuery(query)
	if err != nil {
		return nil, nil
	}
	return a.svc.RankVariantsForEval(ctx, q)
}

// ---- correction mode ----------------------------------------------------

// runCorrection is offline w.r.t. providers: it reads the vocabulary store and
// the library only. Library terms are filtered to those the store recognizes so
// recall measures the correction algorithm, not vocabulary coverage.
func runCorrection(ctx context.Context, pool *pgxpool.Pool, redisClient *goredis.Client, opts options) error {
	vocab := app.BuildVocabularyStore(redisClient)
	if vocab == nil {
		return fmt.Errorf("correction mode needs a vocabulary store (set REDIS_URL)")
	}
	corrector := discoveryService.NewCorrectionService(vocab)

	terms, err := loadLibraryTerms(ctx, pool, opts.limit)
	if err != nil {
		return fmt.Errorf("load library terms: %w", err)
	}
	recognized := filterRecognized(ctx, vocab, terms)
	fmt.Fprintf(os.Stderr, "correction-eval: %d library terms, %d recognized by the vocabulary store\n", len(terms), len(recognized))
	if len(recognized) == 0 {
		return fmt.Errorf("no library terms are in the vocabulary store — run some searches first to seed it")
	}

	once := func() (discoveryEval.HarnessReport, error) {
		return discoveryEval.RunCorrectionEval(ctx, recognized, corrector, opts.typos), nil
	}
	human := func(r discoveryEval.HarnessReport) string {
		return renderCorrection(r.(discoveryEval.CorrectionReport))
	}
	return runHarness("correction", once, human, opts)
}

// filterRecognized keeps only terms the vocabulary store holds exactly — so a
// recall miss means the corrector failed, not that the term was never in vocab.
func filterRecognized(ctx context.Context, vocab discoveryEval.VocabularyLookup, terms []string) []string {
	out := []string{}
	for _, term := range terms {
		if discoveryEval.IsRecognizedTerm(ctx, vocab, term) {
			out = append(out, term)
		}
	}
	return out
}

// ---- merge mode ---------------------------------------------------------

func runMerge(ctx context.Context, cfg *config.Config, pool *pgxpool.Pool, redisClient *goredis.Client, opts options) error {
	entities, err := loadLibraryEntities(ctx, pool, opts.limit, opts.random)
	if err != nil {
		return fmt.Errorf("load library: %w", err)
	}

	progress := func(done, total int) {
		fmt.Fprintf(os.Stderr, "\r  %d/%d (%d%%)", done, total, done*100/total)
		if done == total {
			fmt.Fprintln(os.Stderr)
		}
	}

	once := func() (discoveryEval.HarnessReport, error) {
		fmt.Fprintf(os.Stderr, "merge-eval over %d unique entities (concurrency=%d)...\n", len(entities), opts.concurrency)
		searcher, drain := buildEvalSearcher(cfg, pool, redisClient)
		report := discoveryEval.RunMergeEval(ctx, entities, searcher, opts.concurrency, progress)
		drain()
		return report, nil
	}
	human := func(r discoveryEval.HarnessReport) string { return renderMerge(r.(discoveryEval.MergeReport)) }
	return runHarness("merge", once, human, opts)
}

// ---- eval mode ----------------------------------------------------------

func runEval(ctx context.Context, cfg *config.Config, pool *pgxpool.Pool, redisClient *goredis.Client, opts options) error {
	entities, err := loadLibraryEntities(ctx, pool, opts.limit, opts.random)
	if err != nil {
		return fmt.Errorf("load library: %w", err)
	}

	progress := func(done, total int) {
		fmt.Fprintf(os.Stderr, "\r  %d/%d (%d%%)", done, total, done*100/total)
		if done == total {
			fmt.Fprintln(os.Stderr)
		}
	}

	corpusEnts, mode := corpusEntities(entities, opts.corpus)

	if opts.fixtures != "" {
		return runEvalFixtures(ctx, cfg, pool, corpusEnts, mode, progress, opts)
	}

	once := func() (discoveryEval.HarnessReport, error) {
		fmt.Fprintf(os.Stderr, "evaluating %d entities (corpus=%s, concurrency=%d, top-%d)...\n", len(corpusEnts), opts.corpus, opts.concurrency, opts.topK)
		// nil event store: eval searches must not emit telemetry.
		searcher, drain := buildEvalSearcher(cfg, pool, redisClient)
		report := discoveryEval.RunLibraryEvalMode(ctx, corpusEnts, searcher, opts.concurrency, opts.topK, mode, progress)
		drain() // drain best-effort background writes before exit
		return report, nil
	}
	human := func(r discoveryEval.HarnessReport) string { return renderEval(r.(discoveryEval.EvalReport)) }
	return runHarness("eval", once, human, opts)
}

// runEvalFixtures runs the ranking eval against recorded provider fixtures. With
// -record it captures live provider responses through one shared recording
// Service (concurrent) and writes a single corpus fixture. Without -record it
// replays them through one combined Replayer + one Service, deterministically.
func runEvalFixtures(
	ctx context.Context,
	cfg *config.Config,
	pool *pgxpool.Pool,
	corpusEnts []discoveryEval.LibraryEntity,
	mode discoveryEval.QueryMode,
	progress func(done, total int),
	opts options,
) error {
	human := func(r discoveryEval.HarnessReport) string { return renderEval(r.(discoveryEval.EvalReport)) }

	if opts.record {
		if err := os.MkdirAll(opts.fixtures, 0o755); err != nil {
			return fmt.Errorf("mkdir fixtures %s: %w", opts.fixtures, err)
		}
		// Record writes one corpus file at the end, so the \r progress counter gives
		// no mid-run signal. Emit newline-terminated progress so a tail of the log
		// shows how far a long background record has gotten.
		recProgress := func(done, total int) {
			if done == total || done%25 == 0 {
				fmt.Fprintf(os.Stderr, "recorded %d/%d\n", done, total)
			}
		}
		once := func() (discoveryEval.HarnessReport, error) {
			fmt.Fprintf(os.Stderr, "RECORDING %d entities to %s (corpus=%s, concurrency=%d, top-%d)...\n", len(corpusEnts), opts.fixtures, opts.corpus, opts.concurrency, opts.topK)
			return recordCorpus(ctx, cfg, pool, opts.fixtures, corpusEnts, mode, opts.concurrency, opts.topK, recProgress)
		}
		return runHarness("eval", once, human, opts)
	}

	once := func() (discoveryEval.HarnessReport, error) {
		fmt.Fprintf(os.Stderr, "REPLAYING %d entities from %s (corpus=%s, concurrency=%d, top-%d)...\n", len(corpusEnts), opts.fixtures, opts.corpus, opts.concurrency, opts.topK)
		searcher, err := buildReplaySearcher(cfg, pool, opts.fixtures)
		if err != nil {
			return discoveryEval.EvalReport{}, err
		}
		report := discoveryEval.RunLibraryEvalMode(ctx, corpusEnts, searcher, opts.concurrency, opts.topK, mode, progress)
		return report, nil
	}
	return runHarness("eval", once, human, opts)
}

// buildEvalSearcher constructs the search pipeline as the eval's narrow
// Searcher, plus a drain func to flush its best-effort background writes.
func buildEvalSearcher(cfg *config.Config, pool *pgxpool.Pool, redisClient *goredis.Client) (discoveryEval.Searcher, func()) {
	svc := app.BuildSearchService(cfg, pool, redisClient, nil)
	return searchAdapter{svc: svc}, svc.WaitForBackground
}

// runQuery is the diagnostic mode: dump the top results a single query returns
// through the chosen pipeline, so v1 and v2 can be compared title-by-title.
func runQuery(ctx context.Context, cfg *config.Config, pool *pgxpool.Pool, redisClient *goredis.Client, opts options) error {
	searcher, drain := buildEvalSearcher(cfg, pool, redisClient)
	results, err := searcher.Search(ctx, opts.query)
	drain()
	if err != nil {
		return fmt.Errorf("search %q: %w", opts.query, err)
	}

	n := 6
	if len(results) < n {
		n = len(results)
	}
	fmt.Printf("\n# %q — %d results, top %d:\n", opts.query, len(results), n)
	for i := 0; i < n; i++ {
		r := results[i]
		srcs := make([]string, 0, len(r.Sources))
		for _, s := range r.Sources {
			srcs = append(srcs, s.Provider.String())
		}
		fmt.Printf("  %d. [%-6s] %-45q sub=%-28q src=%v\n", i+1, r.Kind.String(), r.Title, r.Subtitle, srcs)
	}
	return nil
}

func evalQuery(query string) (*domain.SearchQuery, error) {
	kinds := map[domain.ResultKind]bool{
		domain.ResultKindArtist: true,
		domain.ResultKindAlbum:  true,
		domain.ResultKindTrack:  true,
	}
	return domain.NewSearchQuery(query, kinds, 20)
}

// searchAdapter wraps the search service as the eval's narrow Searcher.
// Blended view (all music kinds), no history persistence, synthetic zero user.
type searchAdapter struct {
	svc *discoveryService.Service
}

func (a searchAdapter) Search(ctx context.Context, query string) ([]domain.SearchResult, error) {
	q, err := evalQuery(query)
	if err != nil {
		return nil, err
	}
	out, err := a.svc.Execute(ctx, shared.UserId{}, q, false)
	if err != nil {
		return nil, err
	}
	return out.Results, nil
}

// ---- artist-intent mode -------------------------------------------------

// runArtistIntent evaluates bare-artist-name queries against the artist-card
// oracle — the corpus the library eval (track-only) is blind to. Corpus is the
// distinct library artists; same deterministic sample discipline as eval.
func runArtistIntent(ctx context.Context, cfg *config.Config, pool *pgxpool.Pool, redisClient *goredis.Client, opts options) error {
	artists, err := loadDistinctArtists(ctx, pool, 0)
	if err != nil {
		return fmt.Errorf("load artists: %w", err)
	}
	// corpus=hard isolates the bug's actual home: single-token artist names, where
	// bare-token relevance ties at 1.0 and the kind-blind tiebreak can bury the
	// artist under a same-name track. Multi-token names engage the IDF path and
	// don't tie, so they never exhibit the burial. Filter BEFORE -limit so the
	// limit caps the single-token corpus, not the full list.
	if opts.corpus == "hard" {
		single := artists[:0:0]
		for _, a := range artists {
			if discoveryEval.TokenCount(a) == 1 {
				single = append(single, a)
			}
		}
		artists = single
	}
	if opts.limit > 0 && len(artists) > opts.limit {
		artists = artists[:opts.limit]
	}

	progress := func(done, total int) {
		fmt.Fprintf(os.Stderr, "\r  %d/%d (%d%%)", done, total, done*100/total)
		if done == total {
			fmt.Fprintln(os.Stderr)
		}
	}
	corpusLabel := ""
	if opts.corpus == "hard" {
		corpusLabel = "hard"
	}
	human := func(r discoveryEval.HarnessReport) string {
		return renderArtistIntent(r.(discoveryEval.ArtistIntentReport))
	}

	// Fixtures path: record the artist queries' provider traffic, or replay it
	// deterministically — the seam that lets a ranking change (e.g. cross-kind
	// prominence) be A/B'd over frozen provider responses, free of iTunes-dropout noise.
	if opts.fixtures != "" {
		if opts.record {
			if err := os.MkdirAll(opts.fixtures, 0o755); err != nil {
				return fmt.Errorf("mkdir fixtures %s: %w", opts.fixtures, err)
			}
			recProgress := func(done, total int) {
				if done == total || done%25 == 0 {
					fmt.Fprintf(os.Stderr, "recorded %d/%d\n", done, total)
				}
			}
			once := func() (discoveryEval.HarnessReport, error) {
				fmt.Fprintf(os.Stderr, "RECORDING %d artists to %s (corpus=%s, concurrency=%d, top-%d)...\n", len(artists), opts.fixtures, opts.corpus, opts.concurrency, opts.topK)
				return recordArtistCorpus(ctx, cfg, pool, opts.fixtures, artists, corpusLabel, opts.concurrency, opts.topK, recProgress)
			}
			return runHarness("artist-intent", once, human, opts)
		}
		once := func() (discoveryEval.HarnessReport, error) {
			fmt.Fprintf(os.Stderr, "REPLAYING %d artists from %s (corpus=%s, prominence=%v, concurrency=%d, top-%d)...\n", len(artists), opts.fixtures, opts.corpus, cfg.CrossKindProminenceEnabled, opts.concurrency, opts.topK)
			searcher, err := buildReplaySearcher(cfg, pool, opts.fixtures)
			if err != nil {
				return discoveryEval.ArtistIntentReport{}, err
			}
			report := discoveryEval.RunArtistIntentEval(ctx, artists, searcher, opts.concurrency, opts.topK, corpusLabel, progress)
			return report, nil
		}
		return runHarness("artist-intent", once, human, opts)
	}

	once := func() (discoveryEval.HarnessReport, error) {
		fmt.Fprintf(os.Stderr, "artist-intent over %d distinct artists (corpus=%s, concurrency=%d, top-%d)...\n", len(artists), opts.corpus, opts.concurrency, opts.topK)
		searcher, drain := buildEvalSearcher(cfg, pool, redisClient)
		report := discoveryEval.RunArtistIntentEval(ctx, artists, searcher, opts.concurrency, opts.topK, corpusLabel, progress)
		drain()
		return report, nil
	}
	return runHarness("artist-intent", once, human, opts)
}

// ---- corpus-build mode --------------------------------------------------

// runCorpusBuild materializes the self-growing behavioral eval corpus on demand
// (the same thing the API's nightly job does, runnable now for validation). It
// mines search→engagement labels from discovery_events — completed/library_add ⇒
// +1, wrong_album ⇒ −1 — over the -since-days window, prints a summary + samples,
// and writes the corpus to -json when given. Report-only; never gates.
func runCorpusBuild(ctx context.Context, pool *pgxpool.Pool, opts options) error {
	eventStore := discoveryPersistence.NewPgxEventStore(pool)
	builder := discoveryEval.NewCorpusBuilder(eventStore)

	since := time.Now().UTC().AddDate(0, 0, -opts.sinceDays)
	corpus, err := builder.Build(ctx, since, since.Format("2006-01-02"))
	if err != nil {
		return fmt.Errorf("build behavioral corpus: %w", err)
	}

	pos, neg := corpus.Positives(), corpus.Negatives()
	fmt.Printf("# Discovery behavioral corpus — %s\n\n", time.Now().UTC().Format(time.RFC3339))
	fmt.Printf("- Window: last %d days (since %s)\n", opts.sinceDays, since.Format("2006-01-02"))
	fmt.Printf("- Entries: %d (%d positive, %d negative)\n", len(corpus.Entries), len(pos), len(neg))
	if len(corpus.Entries) == 0 {
		fmt.Printf("\n_Empty — no completed / library_add / wrong_album events in the window yet._\n")
		fmt.Printf("_The corpus grows as users engage; re-run after more usage (or widen -since-days)._\n")
	} else {
		fmt.Printf("\n## Sample labels\n\n")
		for i, e := range corpus.Entries {
			if i >= 12 {
				break
			}
			sign := "+"
			if e.Polarity < 0 {
				sign = "−"
			}
			fmt.Printf("  %s %-24q → [%s] %q — %s\n", sign, e.Query, "", e.Title, e.Subtitle)
		}
	}

	if opts.jsonPath != "" {
		if err := discoveryEval.NewCorpusBuilder(eventStore).Materialize(ctx, since, since.Format("2006-01-02"), opts.jsonPath); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "wrote behavioral corpus to %s\n", opts.jsonPath)
	}
	return nil
}

// ---- signal-a mode ------------------------------------------------------

func runSignalA(ctx context.Context, pool *pgxpool.Pool, redisClient *goredis.Client, opts options) error {
	eventStore := discoveryPersistence.NewPgxEventStore(pool)

	// A correction filter drops misspellings from the strong gaps; without a
	// vocabulary store it is simply skipped (every zero-result counts as a gap).
	var svc *discoveryEval.CoverageSignalAService
	if vocab := app.BuildVocabularyStore(redisClient); vocab != nil {
		svc = discoveryEval.NewCoverageSignalAService(eventStore, discoveryService.NewCorrectionService(vocab))
	} else {
		svc = discoveryEval.NewCoverageSignalAService(eventStore, nil)
	}

	once := func() (discoveryEval.HarnessReport, error) {
		since := time.Now().UTC().AddDate(0, 0, -opts.sinceDays)
		return svc.Execute(ctx, since, opts.top)
	}
	human := func(r discoveryEval.HarnessReport) string {
		return renderSignalA(r.(*discoveryEval.CoverageReportA), opts.sinceDays)
	}
	return runHarness("signal-a", once, human, opts)
}

// ---- signal-b mode ------------------------------------------------------

func runSignalB(ctx context.Context, cfg *config.Config, pool *pgxpool.Pool, opts options) error {
	artists, err := loadDistinctArtists(ctx, pool, opts.limit)
	if err != nil {
		return fmt.Errorf("load artists: %w", err)
	}
	svc := discoveryEval.NewCoverageSignalBService(app.BuildConsensusProviders(cfg, nil))

	once := func() (discoveryEval.HarnessReport, error) {
		fmt.Fprintf(os.Stderr, "scanning %d distinct artists across providers (concurrency=%d)...\n", len(artists), opts.concurrency)
		return svc.Execute(ctx, artists, opts.concurrency)
	}
	human := func(r discoveryEval.HarnessReport) string {
		return renderSignalB(r.(*discoveryEval.CoverageReportB))
	}
	return runHarness("signal-b", once, human, opts)
}

// ---- consensus mode ------------------------------------------------------

// runConsensus has two paths. With -query it is the single-artist diagnostic:
// resolve the Deezer artist, seed its albums, build consensus across providers,
// and dump the per-album verdicts. Without -query it is the report-only
// completeness gauge: build consensus for a corpus of library artists and report
// the mean confirmed fraction (plan 2026-06-24-001 — health tier, never gated).
func runConsensus(ctx context.Context, cfg *config.Config, pool *pgxpool.Pool, opts options) error {
	httpClient := &http.Client{Timeout: 10 * time.Second}
	deezer := providers.NewDeezerAdapter(httpClient)

	var copts []discoveryService.ConsensusOption
	if cfg.HasMusicBrainz() {
		mb := providers.NewMusicBrainzAdapter(httpClient, cfg.MusicBrainzUserAgent)
		copts = append(copts, discoveryService.WithMBAuthority(mb))
	}
	svc := discoveryService.NewConsensusService(app.BuildConsensusProviders(cfg, nil), copts...)

	if opts.query != "" {
		return consensusSingle(ctx, deezer, svc, opts.query)
	}
	return consensusCompleteness(ctx, deezer, svc, pool, opts.limit)
}

// buildArtistConsensus resolves the Deezer artist, seeds with its albums, and
// builds cross-provider consensus — the per-artist core shared by both paths.
func buildArtistConsensus(ctx context.Context, deezer *providers.DeezerAdapter, svc *discoveryService.ConsensusService, artistName string) ([]discoveryService.ConsensusAlbum, string, error) {
	artistResults, err := deezer.Search(ctx, artistName, map[domain.ResultKind]bool{domain.ResultKindArtist: true})
	if err != nil {
		return nil, "", fmt.Errorf("deezer artist search: %w", err)
	}
	artistID := ""
	for _, r := range artistResults {
		for _, s := range r.Sources {
			if s.Provider == domain.ProviderDeezer {
				artistID = s.ExternalID
				break
			}
		}
		if artistID != "" {
			break
		}
	}

	var primaryAlbums []domain.SearchResult
	if artistID != "" {
		primaryAlbums, err = deezer.GetArtistAlbums(ctx, domain.ProviderDeezer, artistID)
		if err != nil {
			return nil, artistID, fmt.Errorf("deezer artist albums: %w", err)
		}
	}
	return svc.BuildConsensus(ctx, artistName, primaryAlbums), artistID, nil
}

func consensusSingle(ctx context.Context, deezer *providers.DeezerAdapter, svc *discoveryService.ConsensusService, artistName string) error {
	results, artistID, err := buildArtistConsensus(ctx, deezer, svc, artistName)
	if err != nil {
		return err
	}
	conf, unconf, rej := tallyConsensus(results)
	fmt.Printf("\n# Consensus for %q — Deezer id %q\n", artistName, artistID)
	fmt.Printf("  %d confirmed · %d unconfirmed · %d rejected (of %d total)\n\n", conf, unconf, rej, len(results))
	for _, r := range results {
		fmt.Printf("  [%-11s] %-48q %s\n", string(r.Status), r.Album.Title, r.Reason)
	}
	return nil
}

// consensusCompleteness is the report-only health gauge: mean confirmed fraction
// across a corpus of library artists. Never gated — provider availability moves
// it run to run.
func consensusCompleteness(ctx context.Context, deezer *providers.DeezerAdapter, svc *discoveryService.ConsensusService, pool *pgxpool.Pool, limit int) error {
	artists, err := loadDistinctArtists(ctx, pool, limit)
	if err != nil {
		return fmt.Errorf("load artists: %w", err)
	}
	fmt.Fprintf(os.Stderr, "consensus completeness over %d artists...\n", len(artists))

	scanned, totalAlbums, totalConfirmed := 0, 0, 0
	var sumFraction float64
	for i, artist := range artists {
		results, _, err := buildArtistConsensus(ctx, deezer, svc, artist)
		if err != nil || len(results) == 0 {
			continue
		}
		conf, _, _ := tallyConsensus(results)
		scanned++
		totalAlbums += len(results)
		totalConfirmed += conf
		sumFraction += float64(conf) / float64(len(results))
		if (i+1)%10 == 0 {
			fmt.Fprintf(os.Stderr, "\r  %d/%d", i+1, len(artists))
		}
	}
	fmt.Fprintln(os.Stderr)

	meanFraction := 0.0
	if scanned > 0 {
		meanFraction = sumFraction / float64(scanned)
	}
	fmt.Printf("# Discovery consensus completeness (report-only) — %s\n\n", time.Now().UTC().Format(time.RFC3339))
	fmt.Printf("- Artists scanned: %d\n", scanned)
	fmt.Printf("- Mean confirmed fraction: %.1f%% (per-artist average)\n", meanFraction*100)
	fmt.Printf("- Pooled: %d confirmed of %d albums (%.1f%%)\n", totalConfirmed, totalAlbums, pooledPct(totalConfirmed, totalAlbums))
	fmt.Printf("\n_Report-only — provider availability moves this run to run; it never gates._\n")
	return nil
}

func tallyConsensus(results []discoveryService.ConsensusAlbum) (conf, unconf, rej int) {
	for _, r := range results {
		switch r.Status {
		case discoveryService.ConsensusConfirmed:
			conf++
		case discoveryService.ConsensusUnconfirmed:
			unconf++
		case discoveryService.ConsensusRejected:
			rej++
		}
	}
	return conf, unconf, rej
}

func pooledPct(n, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(n) / float64(total) * 100
}
