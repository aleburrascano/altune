package main

import (
	"altune/go-api/internal/app"
	"altune/go-api/internal/discovery"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/database"
	shared "altune/go-api/internal/shared/kernel"
	"altune/go-api/internal/shared/logging"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"time"

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
	corpusFile      string
	goldenCorpus    bool
	metricsPath     string
	reportsDir      string
	fixtures        string
	record          bool
}

func main() {
	var opts options
	flag.StringVar(&opts.mode, "mode", "eval", "eval | merge | correction | correction-seed | diversity | health | signal-a | signal-b | artwork | artist-intent | golden | corpus-build | corpus-snapshot | replay | report | detail | detail-live | prod")
	flag.IntVar(&opts.limit, "limit", 0, "eval: max entities to evaluate (0 = all)")
	flag.IntVar(&opts.concurrency, "concurrency", 4, "eval: parallel searches against live providers")
	flag.IntVar(&opts.sinceDays, "since-days", 30, "signals: telemetry window in days")
	flag.IntVar(&opts.top, "top", 50, "signal-a: max entries listed per section in the printed report (counts and gates always cover the full set)")
	flag.IntVar(&opts.topK, "top-k", 3, "eval: top-K window — entity passes if it ranks within the top K (1 = strict #1)")
	flag.StringVar(&opts.jsonPath, "json", "", "write the full JSON report to this path (default: stdout summary only)")
	flag.BoolVar(&opts.random, "random", false, "eval: sample entities randomly instead of alphabetically (use with -limit for a representative sample)")
	flag.StringVar(&opts.query, "query", "", "diagnostic: run a single query and dump the top results (bypasses the library eval)")
	flag.StringVar(&opts.baselinesPath, "baselines", "cmd/discoveryeval/baselines.json", "path to the committed baselines/thresholds file")
	flag.BoolVar(&opts.updateBaselines, "update-baselines", false, "re-baseline: measure the current value(s) and write them to -baselines (explicit, reviewed)")
	flag.IntVar(&opts.noiseRuns, "noise-runs", 1, "with -update-baselines: run N times and set the margin to the measured spread (use 3)")
	flag.IntVar(&opts.typos, "typos", 3, "correction: synthetic typos generated per known-good term")
	flag.StringVar(&opts.corpus, "corpus", "exact", "eval/diversity corpus: exact (\"artist title\") | hard (single-token titles, title-only query)")
	flag.StringVar(&opts.corpusFile, "corpus-file", "", "read the gated corpus from this frozen snapshot instead of the live tracks table; with -mode corpus-snapshot, the path to write; with -mode replay, the behavioral corpus to load (see -mode corpus-build)")
	flag.BoolVar(&opts.goldenCorpus, "corpus-golden", false, "use the track cases of the committed golden set as the library corpus (public, no user data); excludes -corpus-file")
	flag.StringVar(&opts.metricsPath, "metrics", "", "write this run's flat metric list to this path (for -mode report)")
	flag.StringVar(&opts.reportsDir, "reports", "", "report: directory of metrics-*.json files to aggregate and gate")
	flag.StringVar(&opts.fixtures, "fixtures", "", "eval: directory of recorded provider fixtures. With -record, write them (live); without, replay them (deterministic, offline w.r.t. providers)")
	flag.BoolVar(&opts.record, "record", false, "eval: with -fixtures, record provider responses to the directory instead of replaying (runs live, sequentially)")
	flag.Parse()

	opts.sinceDays = clampSinceDays(os.Stderr, opts.sinceDays)

	if err := run(opts); err != nil {
		if errors.Is(err, errRegressed) {
			fmt.Fprintln(os.Stderr, "discoveryeval: REGRESSION")
			os.Exit(2)
		}
		fmt.Fprintf(os.Stderr, "discoveryeval: %v\n", err)
		os.Exit(1)
	}
}

const aggregateRetentionDays = int(discovery.AggregateEventRetention / (24 * time.Hour))

func clampSinceDays(notice io.Writer, sinceDays int) int {
	if sinceDays <= aggregateRetentionDays {
		return sinceDays
	}
	_, _ = fmt.Fprintf(notice, "discoveryeval: -since-days %d exceeds the %d-day aggregate retention window; clamping to %d (older telemetry has been pruned)\n", sinceDays, aggregateRetentionDays, aggregateRetentionDays)
	return aggregateRetentionDays
}

var knownModes = []string{"eval", "merge", "correction", "correction-seed", "diversity", "health", "signal-a", "signal-b", "artwork", "artist-intent", "golden", "corpus-build", "corpus-snapshot", "replay", "report", "detail", "detail-live", "prod"}

func unknownModeError(mode string) error {
	return fmt.Errorf("unknown mode %q (want %s)", mode, strings.Join(knownModes, " | "))
}

func run(opts options) error {
	if !slices.Contains(knownModes, opts.mode) {
		return unknownModeError(opts.mode)
	}
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	logging.Setup(cfg.LogLevel, cfg.IsDevelopment())

	ctx := context.Background()

	if opts.mode == "detail" {
		return runDetail(ctx, cfg, opts)
	}

	if opts.mode == "report" {
		return runReport(opts)
	}

	pool, err := database.NewPool(ctx, cfg.DatabaseURL, cfg.DBPoolMaxConns)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer pool.Close()

	var redisClient *goredis.Client
	if cfg.RedisURL != "" {
		redisClient = sharedRedis.NewClient(ctx, cfg.RedisURL, cfg.RedisPoolSize)
		defer func() { _ = redisClient.Close() }()
	}

	if opts.query != "" {
		return runQuery(ctx, cfg, pool, redisClient, opts)
	}

	switch opts.mode {
	case "eval":
		return runEval(ctx, cfg, pool, redisClient, opts)
	case "detail-live":
		return runDetailLive(ctx, cfg, pool, redisClient, opts)
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
	case "artwork":
		return runArtworkEval(ctx, cfg, pool, redisClient, opts)
	case "artist-intent":
		return runArtistIntent(ctx, cfg, pool, redisClient, opts)
	case "golden":
		return runGolden(ctx, cfg, pool, redisClient, opts)
	case "corpus-build":
		return runCorpusBuild(ctx, pool, opts)
	case "corpus-snapshot":
		return runCorpusSnapshot(ctx, pool, opts)
	case "correction-seed":
		return runCorrectionSeed(ctx, pool, redisClient, opts)
	case "replay":
		return runReplay(ctx, cfg, pool, redisClient, opts)
	case "prod":
		return runProd(ctx, pool, opts)
	default:
		return unknownModeError(opts.mode)
	}
}

func runHealth(ctx context.Context, cfg *config.Config, pool *pgxpool.Pool, redisClient *goredis.Client, opts options) error {
	entities, err := resolveEntities(ctx, pool, opts)
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
	searcher, drain := buildEvalSearcher(ctx, cfg, pool, redisClient)
	report := discovery.RunHealthEval(ctx, entities, searcher, opts.concurrency, progress)
	drain()

	if err := maybeWriteJSON(opts.jsonPath, report); err != nil {
		return err
	}
	fmt.Print(renderHealth(report))

	if opts.updateBaselines {
		existing, err := loadBaselines(opts.baselinesPath)
		if err != nil {
			return err
		}
		if existing == nil {
			existing = discovery.Baselines{}
		}
		for k, v := range discovery.BuildBaselines(report.HealthMetrics(), nil) {
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

func runDiversity(ctx context.Context, cfg *config.Config, pool *pgxpool.Pool, redisClient *goredis.Client, opts options) error {
	entities, err := resolveEntities(ctx, pool, opts)
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
	once := func() (discovery.HarnessReport, error) {
		fmt.Fprintf(os.Stderr, "diversity differential over %d entities (corpus=%s, concurrency=%d, top-%d)...\n", len(corpusEnts), opts.corpus, opts.concurrency, opts.topK)
		svc := app.BuildSearchService(ctx, cfg, pool, redisClient, nil)
		report, err := measureDiversity(ctx, corpusEnts, variantSearchAdapter{svc: svc}, opts.concurrency, opts.topK, mode, progress)
		svc.WaitForBackground()
		if err != nil {
			return nil, err
		}
		return report, nil
	}
	human := func(r discovery.HarnessReport) string { return renderDiversity(r.(discovery.DiversityReport)) }
	return runHarness("diversity", once, human, opts)
}

func measureMerge(ctx context.Context, entities []discovery.LibraryEntity, searcher discovery.Searcher, concurrency int, progress func(done, total int)) (discovery.MergeReport, error) {
	report := discovery.RunMergeEval(ctx, entities, searcher, concurrency, progress)
	return report, requireMeasured("merge", report.Evaluated, report.Errored)
}

type unansweredCounter struct {
	inner      discovery.VariantSearcher
	unanswered atomic.Int64
}

func (c *unansweredCounter) SearchVariants(ctx context.Context, query string) ([]discovery.SearchResult, []discovery.SearchResult) {
	with, without := c.inner.SearchVariants(ctx, query)
	if len(with) == 0 && len(without) == 0 {
		c.unanswered.Add(1)
	}
	return with, without
}

func measureDiversity(ctx context.Context, entities []discovery.LibraryEntity, inner discovery.VariantSearcher, concurrency, k int, mode discovery.QueryMode, progress func(done, total int)) (discovery.DiversityReport, error) {
	counter := &unansweredCounter{inner: inner}
	report := discovery.RunDiversityEvalMode(ctx, entities, counter, concurrency, k, mode, progress)
	return report, requireMeasured("diversity", report.Evaluated, int(counter.unanswered.Load()))
}

func corpusEntities(entities []discovery.LibraryEntity, corpus string) ([]discovery.LibraryEntity, discovery.QueryMode) {
	if corpus != "hard" {
		return entities, discovery.QueryExact
	}
	out := []discovery.LibraryEntity{}
	for _, e := range entities {
		if discovery.TokenCount(e.Title) == 1 {
			out = append(out, e)
		}
	}
	return out, discovery.QueryTitleOnly
}

type variantSearchAdapter struct {
	svc *discovery.SearchService
}

func (a variantSearchAdapter) SearchVariants(ctx context.Context, query string) ([]discovery.SearchResult, []discovery.SearchResult) {
	q, err := evalQuery(query)
	if err != nil {
		return nil, nil
	}
	return a.svc.RankVariantsForEval(ctx, q)
}

func runCorrection(ctx context.Context, pool *pgxpool.Pool, redisClient *goredis.Client, opts options) error {
	vocab := app.BuildVocabularyStore(redisClient)
	if vocab == nil {
		return fmt.Errorf("correction mode needs a vocabulary store (set REDIS_URL)")
	}
	corrector := discovery.NewCorrectionService(vocab)

	terms, err := resolveTerms(ctx, pool, opts)
	if err != nil {
		return fmt.Errorf("load library terms: %w", err)
	}
	recognized := filterRecognized(ctx, vocab, terms)
	fmt.Fprintf(os.Stderr, "correction-eval: %d library terms, %d recognized by the vocabulary store\n", len(terms), len(recognized))
	if len(recognized) == 0 {
		return fmt.Errorf("no library terms are in the vocabulary store — run some searches first to seed it")
	}

	once := func() (discovery.HarnessReport, error) {
		return discovery.RunCorrectionEval(ctx, recognized, corrector, opts.typos), nil
	}
	human := func(r discovery.HarnessReport) string {
		return renderCorrection(r.(discovery.CorrectionReport))
	}
	return runHarness("correction", once, human, opts)
}

func filterRecognized(ctx context.Context, vocab discovery.VocabularyLookup, terms []string) []string {
	out := []string{}
	for _, term := range terms {
		if discovery.IsRecognizedTerm(ctx, vocab, term) {
			out = append(out, term)
		}
	}
	return out
}

func runMerge(ctx context.Context, cfg *config.Config, pool *pgxpool.Pool, redisClient *goredis.Client, opts options) error {
	entities, err := resolveEntities(ctx, pool, opts)
	if err != nil {
		return fmt.Errorf("load library: %w", err)
	}

	progress := func(done, total int) {
		fmt.Fprintf(os.Stderr, "\r  %d/%d (%d%%)", done, total, done*100/total)
		if done == total {
			fmt.Fprintln(os.Stderr)
		}
	}

	once := func() (discovery.HarnessReport, error) {
		fmt.Fprintf(os.Stderr, "merge-eval over %d unique entities (concurrency=%d)...\n", len(entities), opts.concurrency)
		searcher, drain := buildEvalSearcher(ctx, cfg, pool, redisClient)
		report, err := measureMerge(ctx, entities, searcher, opts.concurrency, progress)
		drain()
		if err != nil {
			return nil, err
		}
		return report, nil
	}
	human := func(r discovery.HarnessReport) string { return renderMerge(r.(discovery.MergeReport)) }
	return runHarness("merge", once, human, opts)
}

func runEval(ctx context.Context, cfg *config.Config, pool *pgxpool.Pool, redisClient *goredis.Client, opts options) error {
	entities, err := resolveEntities(ctx, pool, opts)
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

	once := func() (discovery.HarnessReport, error) {
		fmt.Fprintf(os.Stderr, "evaluating %d entities (corpus=%s, concurrency=%d, top-%d)...\n", len(corpusEnts), opts.corpus, opts.concurrency, opts.topK)
		searcher, drain := buildEvalSearcher(ctx, cfg, pool, redisClient)
		report := discovery.RunLibraryEvalMode(ctx, corpusEnts, searcher, opts.concurrency, opts.topK, mode, progress)
		drain()
		return report, nil
	}
	human := func(r discovery.HarnessReport) string { return renderEval(r.(discovery.EvalReport)) }
	return runHarness("eval", once, human, opts)
}

func runEvalFixtures(
	ctx context.Context,
	cfg *config.Config,
	pool *pgxpool.Pool,
	corpusEnts []discovery.LibraryEntity,
	mode discovery.QueryMode,
	progress func(done, total int),
	opts options,
) error {
	human := func(r discovery.HarnessReport) string { return renderEval(r.(discovery.EvalReport)) }

	if opts.record {
		if err := os.MkdirAll(opts.fixtures, 0o755); err != nil {
			return fmt.Errorf("mkdir fixtures %s: %w", opts.fixtures, err)
		}
		recProgress := func(done, total int) {
			if done == total || done%25 == 0 {
				fmt.Fprintf(os.Stderr, "recorded %d/%d\n", done, total)
			}
		}
		once := func() (discovery.HarnessReport, error) {
			fmt.Fprintf(os.Stderr, "RECORDING %d entities to %s (corpus=%s, concurrency=%d, top-%d)...\n", len(corpusEnts), opts.fixtures, opts.corpus, opts.concurrency, opts.topK)
			return recordCorpus(ctx, cfg, pool, opts.fixtures, corpusEnts, mode, opts.concurrency, opts.topK, recProgress)
		}
		return runHarness("eval", once, human, opts)
	}

	once := func() (discovery.HarnessReport, error) {
		fmt.Fprintf(os.Stderr, "REPLAYING %d entities from %s (corpus=%s, concurrency=%d, top-%d)...\n", len(corpusEnts), opts.fixtures, opts.corpus, opts.concurrency, opts.topK)
		searcher, err := buildReplaySearcher(ctx, cfg, pool, opts.fixtures)
		if err != nil {
			return discovery.EvalReport{}, err
		}
		report := discovery.RunLibraryEvalMode(ctx, corpusEnts, searcher, opts.concurrency, opts.topK, mode, progress)
		return report, nil
	}
	return runHarness("eval", once, human, opts)
}

func buildEvalSearcher(ctx context.Context, cfg *config.Config, pool *pgxpool.Pool, redisClient *goredis.Client) (discovery.Searcher, func()) {
	svc := app.BuildSearchService(ctx, cfg, pool, redisClient, nil)
	return searchAdapter{svc: svc}, svc.WaitForBackground
}

func runQuery(ctx context.Context, cfg *config.Config, pool *pgxpool.Pool, redisClient *goredis.Client, opts options) error {
	searcher, drain := buildEvalSearcher(ctx, cfg, pool, redisClient)
	results, err := searcher.Search(ctx, opts.query)
	drain()
	if err != nil {
		return fmt.Errorf("search %q: %w", opts.query, err)
	}

	n := 20
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
		fmt.Printf("  %d. [%-6s] %-45q sub=%-28q src=%v isrc=%q mbid=%q upc=%q\n", i+1, r.Kind.String(), r.Title, r.Subtitle, srcs, r.ISRC, r.EntityMBID(), r.UPC)
	}
	return nil
}

func evalQuery(query string) (*discovery.SearchQuery, error) {
	kinds := map[discovery.ResultKind]bool{
		discovery.ResultKindArtist: true,
		discovery.ResultKindAlbum:  true,
		discovery.ResultKindTrack:  true,
	}
	return discovery.NewSearchQuery(query, kinds, 20)
}

type searchAdapter struct {
	svc *discovery.SearchService
}

func (a searchAdapter) Search(ctx context.Context, query string) ([]discovery.SearchResult, error) {
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

func runArtistIntent(ctx context.Context, cfg *config.Config, pool *pgxpool.Pool, redisClient *goredis.Client, opts options) error {
	artists, err := resolveArtists(ctx, pool, opts, 0)
	if err != nil {
		return fmt.Errorf("load artists: %w", err)
	}
	if opts.corpus == "hard" {
		single := artists[:0:0]
		for _, a := range artists {
			if discovery.TokenCount(a) == 1 {
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
	human := func(r discovery.HarnessReport) string {
		return renderArtistIntent(r.(discovery.ArtistIntentReport))
	}

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
			once := func() (discovery.HarnessReport, error) {
				fmt.Fprintf(os.Stderr, "RECORDING %d artists to %s (corpus=%s, concurrency=%d, top-%d)...\n", len(artists), opts.fixtures, opts.corpus, opts.concurrency, opts.topK)
				return recordArtistCorpus(ctx, cfg, pool, opts.fixtures, artists, corpusLabel, opts.concurrency, opts.topK, recProgress)
			}
			return runHarness("artist-intent", once, human, opts)
		}
		once := func() (discovery.HarnessReport, error) {
			fmt.Fprintf(os.Stderr, "REPLAYING %d artists from %s (corpus=%s, prominence=%v, concurrency=%d, top-%d)...\n", len(artists), opts.fixtures, opts.corpus, cfg.CrossKindProminenceEnabled, opts.concurrency, opts.topK)
			searcher, err := buildReplaySearcher(ctx, cfg, pool, opts.fixtures)
			if err != nil {
				return discovery.ArtistIntentReport{}, err
			}
			report := discovery.RunArtistIntentEval(ctx, artists, searcher, opts.concurrency, opts.topK, corpusLabel, progress)
			return report, nil
		}
		return runHarness("artist-intent", once, human, opts)
	}

	once := func() (discovery.HarnessReport, error) {
		fmt.Fprintf(os.Stderr, "artist-intent over %d distinct artists (corpus=%s, concurrency=%d, top-%d)...\n", len(artists), opts.corpus, opts.concurrency, opts.topK)
		searcher, drain := buildEvalSearcher(ctx, cfg, pool, redisClient)
		report := discovery.RunArtistIntentEval(ctx, artists, searcher, opts.concurrency, opts.topK, corpusLabel, progress)
		drain()
		return report, nil
	}
	return runHarness("artist-intent", once, human, opts)
}

func runCorpusBuild(ctx context.Context, pool *pgxpool.Pool, opts options) error {
	eventStore := discovery.NewPgxEventStore(pool)
	builder := discovery.NewCorpusBuilder(eventStore)

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
		if err := discovery.NewCorpusBuilder(eventStore).Materialize(ctx, since, since.Format("2006-01-02"), opts.jsonPath); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "wrote behavioral corpus to %s\n", opts.jsonPath)
	}
	return nil
}

func runSignalA(ctx context.Context, pool *pgxpool.Pool, redisClient *goredis.Client, opts options) error {
	eventStore := discovery.NewPgxEventStore(pool)

	var svc *discovery.CoverageSignalAService
	if vocab := app.BuildVocabularyStore(redisClient); vocab != nil {
		svc = discovery.NewCoverageSignalAService(eventStore, discovery.NewCorrectionService(vocab))
	} else {
		svc = discovery.NewCoverageSignalAService(eventStore, nil)
	}

	once := func() (discovery.HarnessReport, error) {
		since := time.Now().UTC().AddDate(0, 0, -opts.sinceDays)
		report, err := svc.Execute(ctx, since, math.MaxInt32)
		if err != nil {
			return nil, err
		}
		return report, requireSignalASamples(report)
	}
	human := func(r discovery.HarnessReport) string {
		return renderSignalA(r.(*discovery.CoverageReportA), opts.sinceDays, opts.top)
	}
	return runHarness("signal-a", once, human, opts)
}

func runSignalB(ctx context.Context, cfg *config.Config, pool *pgxpool.Pool, opts options) error {
	artists, err := resolveArtists(ctx, pool, opts, opts.limit)
	if err != nil {
		return fmt.Errorf("load artists: %w", err)
	}
	svc := discovery.NewCoverageSignalBService(app.BuildConsensusProviders(cfg, nil))

	once := func() (discovery.HarnessReport, error) {
		fmt.Fprintf(os.Stderr, "scanning %d distinct artists across providers (concurrency=%d)...\n", len(artists), opts.concurrency)
		report, err := svc.Execute(ctx, artists, opts.concurrency)
		if err != nil {
			return nil, err
		}
		return report, requireMeasured("signal-b", report.ArtistsScanned, 0)
	}
	human := func(r discovery.HarnessReport) string {
		return renderSignalB(r.(*discovery.CoverageReportB))
	}
	return runHarness("signal-b", once, human, opts)
}
