package main

import (
	"altune/go-api/internal/acquisition/adapters/ffmpegtag"
	"altune/go-api/internal/acquisition/adapters/id3"
	"altune/go-api/internal/catalog/adapters/storage"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/binpath"
	"altune/go-api/internal/shared/config"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"time"

	catalogPorts "altune/go-api/internal/catalog/ports"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	runTimeout         = 6 * time.Hour
	defaultConcurrency = 2
	maxConcurrency     = 16
)

var (
	errRunAndSince       = errors.New("-run and -since cannot be combined")
	errNegativeLimit     = errors.New("-limit cannot be negative")
	errEmptyDatabaseURL  = errors.New("DATABASE_URL is not set")
	errBadConcurrencyEnv = fmt.Errorf("RETAG_CONCURRENCY must be an integer from 1 to %d", maxConcurrency)
)

type options struct {
	apply bool
	limit int
	sel   selection
}

func parseOptions(args []string, output io.Writer) (options, error) {
	flags := flag.NewFlagSet("retag", flag.ContinueOnError)
	flags.SetOutput(output)
	var opts options
	var rawRunID, rawSince string
	flags.BoolVar(&opts.apply, "apply", false, "rewrite the files (default is a dry run)")
	flags.IntVar(&opts.limit, "limit", 0, "stop after this many tracks needing a retag (0 = no limit)")
	flags.StringVar(&rawRunID, "run", "", "only tracks in the change log under this run id")
	flags.StringVar(&rawSince, "since", "", "only tracks in the change log since this RFC3339 time")
	if err := flags.Parse(args); err != nil {
		return options{}, err
	}
	if opts.limit < 0 {
		return options{}, errNegativeLimit
	}
	if rawRunID != "" && rawSince != "" {
		return options{}, errRunAndSince
	}
	var err error
	opts.sel, err = parseSelection(rawRunID, rawSince)
	return opts, err
}

func parseSelection(rawRunID, rawSince string) (selection, error) {
	var sel selection
	var err error
	if rawRunID != "" {
		if sel.runID, err = uuid.Parse(rawRunID); err != nil {
			return selection{}, fmt.Errorf("-run: %w", err)
		}
	}
	if rawSince != "" {
		if sel.since, err = time.Parse(time.RFC3339, rawSince); err != nil {
			return selection{}, fmt.Errorf("-since: %w", err)
		}
	}
	return sel, nil
}

func concurrencyFrom(raw string) (int, error) {
	if raw == "" {
		return defaultConcurrency, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > maxConcurrency {
		return 0, errBadConcurrencyEnv
	}
	return n, nil
}

func openPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	if databaseURL == "" {
		return nil, errEmptyDatabaseURL
	}
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	cfg.MaxConns = 2
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return pool, nil
}

func main() {
	opts, err := parseOptions(os.Args[1:], os.Stderr)
	if errors.Is(err, flag.ErrHelp) {
		os.Exit(0)
	}
	if err == nil {
		err = run(opts, os.Getenv, os.Stdout)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "retag: %v\n", err)
		os.Exit(1)
	}
}

func productionRetagger(cfg *config.Config, audioStore catalogPorts.AudioStore, tracks trackStore, limit int) retagger {
	ffmpeg := binpath.Resolve("ffmpeg", cfg.FFmpegLocation)
	return retagger{
		store:       audioStore,
		tagger:      ffmpegtag.NewTagger(ffmpeg, id3.NewTagger()),
		media:       ffmpegTools{ffprobe: binpath.Resolve("ffprobe", cfg.FFmpegLocation), ffmpeg: ffmpeg},
		attemptID:   shared.RandomID,
		stillServes: tracks.stillServes,
		claim:       newClaim(limit),
	}
}

type runtime struct {
	cfg         *config.Config
	audioStore  catalogPorts.AudioStore
	concurrency int
}

func loadRuntime(env func(string) string) (runtime, error) {
	concurrency, err := concurrencyFrom(env("RETAG_CONCURRENCY"))
	if err != nil {
		return runtime{}, err
	}
	cfg, err := config.Load()
	if err != nil {
		return runtime{}, fmt.Errorf("load config: %w", err)
	}
	audioStore, err := storage.FromConfig(cfg, slog.Default())
	if err != nil {
		return runtime{}, err
	}
	return runtime{cfg: cfg, audioStore: audioStore, concurrency: concurrency}, nil
}

func run(opts options, env func(string) string, out io.Writer) error {
	rt, err := loadRuntime(env)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()
	pool, err := openPool(ctx, rt.cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	tracks := trackStore{pool: pool}
	if !opts.apply {
		tracks.access = pgx.ReadOnly
	}
	r := productionRetagger(rt.cfg, rt.audioStore, tracks, opts.limit)
	return runRetag(ctx, tracks, r, opts.sel, opts.apply, rt.concurrency, out)
}
