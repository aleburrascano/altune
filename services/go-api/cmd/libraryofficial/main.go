package main

import (
	"altune/go-api/internal/acquisition/adapters/ytmsearch"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	runTimeout            = 4 * time.Hour
	searchTimeout         = 10 * time.Second
	defaultConcurrency    = 2
	maxConcurrency        = 16
	defaultMinInterval    = time.Second
	concurrencyVarName    = "LIBRARYOFFICIAL_CONCURRENCY"
	minIntervalVarName    = "LIBRARYOFFICIAL_MIN_INTERVAL"
	databaseURLVarName    = "DATABASE_URL"
	readOnlySessionOption = "default_transaction_read_only"
)

var (
	errEmptyDatabaseURL  = errors.New(databaseURLVarName + " is not set")
	errApplyAndUndo      = errors.New("-apply and -undo are separate modes")
	errUndoWithFilters   = errors.New("-undo takes no -user or -limit")
	errNegativeLimit     = errors.New("-limit must not be negative")
	errBadConcurrencyEnv = fmt.Errorf("%s must be an integer from 1 to %d", concurrencyVarName, maxConcurrency)
	errBadMinIntervalEnv = errors.New(minIntervalVarName + " must be a duration of zero or more, like 1s")
)

type options struct {
	apply     bool
	undo      bool
	undoRunID uuid.UUID
	userID    string
	limit     int
}

func parseOptions(args []string, output io.Writer) (options, error) {
	flags := flag.NewFlagSet("libraryofficial", flag.ContinueOnError)
	flags.SetOutput(output)
	var opts options
	var rawRunID string
	flags.BoolVar(&opts.apply, "apply", false, "write the pointers into catalog_sources (default is a dry run)")
	flags.StringVar(&rawRunID, "undo", "", "remove the pointers this run id wrote where catalog_sources is unchanged since")
	flags.StringVar(&opts.userID, "user", "", "only this user id")
	flags.IntVar(&opts.limit, "limit", 0, "stop after this many tracks (0 means all)")
	if err := flags.Parse(args); err != nil {
		return options{}, err
	}
	if rawRunID != "" {
		runID, err := uuid.Parse(rawRunID)
		if err != nil {
			return options{}, fmt.Errorf("-undo: %w", err)
		}
		opts.undo, opts.undoRunID = true, runID
	}
	return opts, opts.check()
}

func (o options) check() error {
	switch {
	case o.apply && o.undo:
		return errApplyAndUndo
	case o.undo && (o.userID != "" || o.limit != 0):
		return errUndoWithFilters
	case o.limit < 0:
		return errNegativeLimit
	}
	return nil
}

type pacing struct {
	concurrency int
	minInterval time.Duration
}

func pacingFromEnv(env func(string) string) (pacing, error) {
	paced := pacing{concurrency: defaultConcurrency, minInterval: defaultMinInterval}
	if raw := env(concurrencyVarName); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > maxConcurrency {
			return pacing{}, errBadConcurrencyEnv
		}
		paced.concurrency = n
	}
	if raw := env(minIntervalVarName); raw != "" {
		interval, err := time.ParseDuration(raw)
		if err != nil || interval < 0 {
			return pacing{}, errBadMinIntervalEnv
		}
		paced.minInterval = interval
	}
	return paced, nil
}

func openPool(ctx context.Context, databaseURL string, readOnly bool) (*pgxpool.Pool, error) {
	if databaseURL == "" {
		return nil, errEmptyDatabaseURL
	}
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	cfg.MaxConns = 2
	if readOnly {
		cfg.ConnConfig.RuntimeParams[readOnlySessionOption] = "on"
	}
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
		fmt.Fprintf(os.Stderr, "libraryofficial: %v\n", err)
		os.Exit(1)
	}
}

func run(opts options, env func(string) string, out io.Writer) error {
	paced, err := pacingFromEnv(env)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()
	pool, err := openPool(ctx, env(databaseURLVarName), !opts.apply && !opts.undo)
	if err != nil {
		return err
	}
	defer pool.Close()
	s := store{pool: pool}
	if opts.undo {
		return runUndo(ctx, s, opts.undoRunID, out)
	}
	search := ytmsearch.New(ytmsearch.Config{MinInterval: paced.minInterval, Timeout: searchTimeout})
	return runFind(ctx, s, search, paced.concurrency, opts, out)
}

func runUndo(ctx context.Context, s store, runID uuid.UUID, out io.Writer) error {
	tally, err := s.undo(ctx, runID)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(out, "undo %s: removed %d pointers, kept %d (catalog_sources changed since)\n", runID, tally.Restored, tally.Kept)
	return nil
}

func runFind(ctx context.Context, s store, search songSearch, concurrency int, opts options, out io.Writer) error {
	targets, err := s.loadTargets(ctx, opts.userID)
	if err != nil {
		return err
	}
	if opts.limit > 0 {
		targets = targets[:min(len(targets), opts.limit)]
	}
	var sink pointer
	if opts.apply {
		sink = s
	}
	runID := uuid.New()
	done, runErr := runOfficial(ctx, search, targets, sink, concurrency, runID)
	writeReport(out, done, opts.apply, runID)
	return runErr
}
