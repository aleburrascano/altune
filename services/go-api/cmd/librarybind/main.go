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
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	httpTimeout = 20 * time.Second
	runTimeout  = 4 * time.Hour
)

const (
	maxCutGapVarName = "LIBRARYBIND_MAX_CUT_GAP"
	defaultMaxCutGap = 10 * time.Second
)

func cutPolicyFromEnv(env func(string) string) (cutPolicy, error) {
	gap := defaultMaxCutGap
	if raw := env(maxCutGapVarName); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed <= 0 {
			return cutPolicy{}, errBadMaxCutGap
		}
		gap = parsed
	}
	return cutPolicy{maxGapSeconds: gap.Seconds()}, nil
}

const usageNotice = "librarybind: do not run this from the prod VM, Deezer rate-blocks the prod IP and the app shares it; run it from another host, dry runs with a read-only LIBRARYBIND_DATABASE_URL"

var (
	errEmptyDatabaseURL     = errors.New("LIBRARYBIND_DATABASE_URL is not set")
	errApplyAndUndo         = errors.New("-apply and -undo are separate modes")
	errUndoWithFilters      = errors.New("-undo takes no -user or -limit")
	errNegativeLimit        = errors.New("-limit must not be negative")
	errReportWithUndo       = errors.New("-report belongs to a bind run, not -undo")
	errBadMaxCutGap         = errors.New(maxCutGapVarName + " must be a duration above zero, like 10s")
	errPinsWithBindFlag     = errors.New("-pins takes no -undo, -user, -limit or -report")
	errCatalogPinWithUndo   = errors.New("-pin-catalog and -undo are separate modes")
	errCatalogPinWithReport = errors.New("-report belongs to a bind run, not -pin-catalog")
	errCatalogPinWithPins   = errors.New("-pin-catalog and -pins are separate modes")
)

type options struct {
	apply      bool
	pinCatalog bool
	undo       bool
	undoRunID  uuid.UUID
	userID     string
	limit      int
	report     string
	pins       string
}

func parseOptions(args []string, output io.Writer) (options, error) {
	flags := flag.NewFlagSet("librarybind", flag.ContinueOnError)
	flags.SetOutput(output)
	var opts options
	var rawRunID string
	flags.BoolVar(&opts.apply, "apply", false, "write the identity rows (default is a dry run)")
	flags.BoolVar(&opts.pinCatalog, "pin-catalog", false, "set catalog_mbid of bound tracks to their bound recording (dry run unless -apply)")
	flags.StringVar(&rawRunID, "undo", "", "delete the identity rows of this run id whose track version is unchanged, restoring the identity a replacement overwrote, and restore the catalog_mbid it pinned where unchanged")
	flags.StringVar(&opts.userID, "user", "", "only this user id")
	flags.IntVar(&opts.limit, "limit", 0, "stop after this many tracks (0 means all)")
	flags.StringVar(&opts.report, "report", "", "write a libraryrelength fix_isrc plan for the wrong ISRCs this run replaced")
	flags.StringVar(&opts.pins, "pins", "", "write the hand-pinned identities in this JSON file instead of binding by rule")
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
	case o.pins != "" && (o.undo || o.userID != "" || o.limit != 0 || o.report != ""):
		return errPinsWithBindFlag
	case o.pinCatalog && o.pins != "":
		return errCatalogPinWithPins
	case o.pinCatalog && o.undo:
		return errCatalogPinWithUndo
	case o.pinCatalog && o.report != "":
		return errCatalogPinWithReport
	case o.undo && (o.userID != "" || o.limit != 0):
		return errUndoWithFilters
	case o.undo && o.report != "":
		return errReportWithUndo
	case o.limit < 0:
		return errNegativeLimit
	}
	return nil
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
		fmt.Fprintf(os.Stderr, "librarybind: %v\n", err)
		os.Exit(1)
	}
}

func run(opts options, env func(string) string, out io.Writer) error {
	fmt.Fprintln(os.Stderr, usageNotice)
	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()
	pool, err := openPool(ctx, env("LIBRARYBIND_DATABASE_URL"))
	if err != nil {
		return err
	}
	defer pool.Close()
	s := store{pool: pool}
	if !opts.apply && !opts.undo {
		s.access = pgx.ReadOnly
	}
	if opts.undo {
		return runUndo(ctx, s, opts.undoRunID, out)
	}
	if opts.pins != "" {
		return runPinned(ctx, s, opts, out)
	}
	if opts.pinCatalog {
		return runCatalogPin(ctx, s, opts, out)
	}
	return runBind(ctx, s, opts, env, out)
}

func runPinned(ctx context.Context, s store, opts options, out io.Writer) error {
	raw, err := os.ReadFile(opts.pins)
	if err != nil {
		return fmt.Errorf("read pins: %w", err)
	}
	var sink identitySink
	if opts.apply {
		sink = s
	}
	return runPins(ctx, raw, s.loadPinTargets, sink, uuid.New(), out)
}

func runUndo(ctx context.Context, s store, runID uuid.UUID, out io.Writer) error {
	tally, err := s.undoRun(ctx, runID)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(out, "undo %s: removed %d, restored %d, kept %d (track changed since the bind); unpinned %d, kept %d pins (catalog_mbid changed since)\n", runID, tally.Removed, tally.Restored, tally.Kept, tally.Unpinned, tally.PinsKept)
	return nil
}

func runBind(ctx context.Context, s store, opts options, env func(string) string, out io.Writer) error {
	tracks, err := s.loadTracks(ctx, opts.userID, opts.limit)
	if err != nil {
		return err
	}
	held, err := s.loadHeld(ctx, opts.userID)
	if err != nil {
		return err
	}
	lookups, refiners, err := bindWiring(env, held)
	if err != nil {
		return err
	}
	runID := uuid.New()
	done, runErr := bindTracks(ctx, tracks, lookups, sinkFor(s, opts), runID, refiners...)
	writeReport(out, done, opts.apply, runID)
	return errors.Join(runErr, writeReportedPlan(opts, done, runErr))
}

func sinkFor(s store, opts options) identitySink {
	if opts.apply {
		return s
	}
	return nil
}

func writeReportedPlan(opts options, done tally, runErr error) error {
	if opts.report == "" {
		return nil
	}
	return writeRelengthPlan(opts.report, relengthPlan(done.Decisions, runErr != nil))
}

func bindWiring(env func(string) string, held heldIndex) ([]isrcLookup, []refineFunc, error) {
	cuts, err := cutPolicyFromEnv(env)
	if err != nil {
		return nil, nil, err
	}
	httpClient := &http.Client{Timeout: httpTimeout}
	deezer := newDeezerLookup(httpClient, deezerBaseURL, deezerRequestsPerSec)
	musicBrainz := newMusicBrainzLookup(httpClient, musicBrainzBaseURL, musicBrainzRequestsSec)
	adapter := newAppleAdapter(httpClient)
	apple := newAppleLookup(adapter, appleRequestsPerSec)
	artistSources := []artistSource{deezerArtistSource(deezer), appleArtistSource{api: adapter, limiter: apple.limiter}}
	search := searchBinder{musicBrainz: musicBrainz, deezer: deezer, cuts: cuts}
	refiners := []refineFunc{newArtistFirst(artistSources, musicBrainz, held).refine, search.refine, cuts.bindDifferingCut, search.refineDifferingCut, markUncatalogued}
	return []isrcLookup{deezer, musicBrainz, apple}, refiners, nil
}
