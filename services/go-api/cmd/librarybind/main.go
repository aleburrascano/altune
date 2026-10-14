package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	httpTimeout = 20 * time.Second
	runTimeout  = 4 * time.Hour
)

const usageNotice = "librarybind: do not run this from the prod VM, Deezer rate-blocks the prod IP and the app shares it; run it from another host, dry runs with a read-only LIBRARYBIND_DATABASE_URL"

var (
	errEmptyDatabaseURL = errors.New("LIBRARYBIND_DATABASE_URL is not set")
	errApplyAndUndo     = errors.New("-apply and -undo are separate modes")
	errUndoWithFilters  = errors.New("-undo takes no -user or -limit")
	errNegativeLimit    = errors.New("-limit must not be negative")
)

type options struct {
	apply     bool
	undo      bool
	undoRunID uuid.UUID
	userID    string
	limit     int
}

func parseOptions(args []string, output io.Writer) (options, error) {
	flags := flag.NewFlagSet("librarybind", flag.ContinueOnError)
	flags.SetOutput(output)
	var opts options
	var rawRunID string
	flags.BoolVar(&opts.apply, "apply", false, "write the identity rows (default is a dry run)")
	flags.StringVar(&rawRunID, "undo", "", "delete the identity rows of this run id whose track version is unchanged")
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
		fmt.Fprintf(os.Stderr, "librarybind: %v\n", err)
		os.Exit(1)
	}
}

func run(opts options, env func(string) string, out io.Writer) error {
	fmt.Fprintln(os.Stderr, usageNotice)
	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()
	pool, err := openPool(ctx, env("LIBRARYBIND_DATABASE_URL"), !opts.apply && !opts.undo)
	if err != nil {
		return err
	}
	defer pool.Close()
	s := store{pool: pool}
	if opts.undo {
		return runUndo(ctx, s, opts.undoRunID, out)
	}
	return runBind(ctx, s, opts, out)
}

func runUndo(ctx context.Context, s store, runID uuid.UUID, out io.Writer) error {
	tally, err := s.undoRun(ctx, runID)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(out, "undo %s: removed %d, kept %d (track changed since the bind)\n", runID, tally.Removed, tally.Kept)
	return nil
}

func runBind(ctx context.Context, s store, opts options, out io.Writer) error {
	tracks, err := s.loadTracks(ctx, opts.userID, opts.limit)
	if err != nil {
		return err
	}
	httpClient := &http.Client{Timeout: httpTimeout}
	lookups := []isrcLookup{
		newDeezerLookup(httpClient, deezerBaseURL, deezerRequestsPerSec),
		newMusicBrainzLookup(httpClient, musicBrainzBaseURL, musicBrainzRequestsSec),
		newAppleLookup(newAppleAdapter(httpClient), appleRequestsPerSec),
	}
	var sink identitySink
	if opts.apply {
		sink = s
	}
	runID := uuid.New()
	done, runErr := bindTracks(ctx, tracks, lookups, sink, runID)
	writeReport(out, done, opts.apply, runID)
	return runErr
}
