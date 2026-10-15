package main

import (
	"altune/go-api/internal/acquisition/adapters/ytmsearch"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	runTimeout          = 4 * time.Hour
	databaseURLVarName  = "DATABASE_URL"
	minIntervalVarName  = "LIBRARYGRADE_MIN_INTERVAL"
	defaultMinInterval  = 2 * time.Second
	lookupTimeout       = 20 * time.Second
	soundCloudOEmbedURL = "https://soundcloud.com/oembed"
)

var (
	errEmptyDatabaseURL = errors.New(databaseURLVarName + " is not set")
	errBadMinInterval   = errors.New(minIntervalVarName + " must be a duration above zero, like 2s")
	errApplyAndUndo     = errors.New("-apply and -undo are separate modes")
	errUndoWithFilters  = errors.New("-undo takes no -user or -limit")
	errNegativeLimit    = errors.New("-limit must not be negative")
)

type options struct {
	apply     bool
	undo      bool
	undoRunID uuid.UUID
	limit     int
	userID    string
}

func parseOptions(args []string, output io.Writer) (options, error) {
	flags := flag.NewFlagSet("librarygrade", flag.ContinueOnError)
	flags.SetOutput(output)
	var opts options
	var rawRunID string
	flags.BoolVar(&opts.apply, "apply", false, "write the source kind and grade (default is a dry run)")
	flags.StringVar(&rawRunID, "undo", "", "remove what this run id wrote")
	flags.StringVar(&opts.userID, "user", "", "only this user id")
	flags.IntVar(&opts.limit, "limit", 0, "stop after this many tracks (0 is all)")
	if err := flags.Parse(args); err != nil {
		return options{}, err
	}
	switch {
	case opts.limit < 0:
		return options{}, errNegativeLimit
	case rawRunID != "":
		return undoOptions(opts, rawRunID)
	}
	return opts, nil
}

func undoOptions(opts options, rawRunID string) (options, error) {
	runID, err := uuid.Parse(rawRunID)
	if err != nil {
		return options{}, fmt.Errorf("-undo: %w", err)
	}
	switch {
	case opts.apply:
		return options{}, errApplyAndUndo
	case opts.limit != 0 || opts.userID != "":
		return options{}, errUndoWithFilters
	}
	return options{undo: true, undoRunID: runID}, nil
}

func minIntervalFromEnv(env func(string) string) (time.Duration, error) {
	raw := env(minIntervalVarName)
	if raw == "" {
		return defaultMinInterval, nil
	}
	interval, err := time.ParseDuration(raw)
	if err != nil || interval <= 0 {
		return 0, errBadMinInterval
	}
	return interval, nil
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
		cfg.ConnConfig.RuntimeParams["default_transaction_read_only"] = "on"
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
		fmt.Fprintf(os.Stderr, "librarygrade: %v\n", err)
		os.Exit(1)
	}
}

func run(opts options, env func(string) string, out io.Writer) error {
	interval, err := minIntervalFromEnv(env)
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
	if opts.undo {
		return runUndo(ctx, store{pool: pool}, opts.undoRunID, out)
	}
	youtube := ytmsearch.New(ytmsearch.Config{MinInterval: interval, Timeout: lookupTimeout})
	return runBackfill(ctx, store{pool: pool}, newSourceLookup(youtube, interval), opts, uuid.New(), out)
}
