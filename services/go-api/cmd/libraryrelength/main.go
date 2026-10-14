package main

import (
	"altune/go-api/internal/acquisition/adapters/persistence"
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
	runTimeout       = 30 * time.Minute
	defaultMaxPerRun = 10
)

var (
	errEmptyDatabaseURL = errors.New("RELENGTH_DATABASE_URL is not set")
	errNoMode           = errors.New("give -report <path> (dry run, or with -apply), -status or -undo <run_id>")
	errOneMode          = errors.New("-report, -status and -undo are separate modes")
	errApplyNoReport    = errors.New("-apply needs -report <path>")
)

type options struct {
	reportPath string
	apply      bool
	status     bool
	undo       bool
	undoRunID  uuid.UUID
}

func parseOptions(args []string, output io.Writer) (options, error) {
	flags := flag.NewFlagSet("libraryrelength", flag.ContinueOnError)
	flags.SetOutput(output)
	var opts options
	var rawRunID string
	flags.StringVar(&opts.reportPath, "report", "", "libraryaudit JSON report to fix or re-acquire from")
	flags.BoolVar(&opts.apply, "apply", false, "write the changes and enqueue the replace jobs (default is a dry run)")
	flags.BoolVar(&opts.status, "status", false, "show the outcome of every enqueued re-acquire and the listen list")
	flags.StringVar(&rawRunID, "undo", "", "restore the old values of this run id")
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
	modes := 0
	for _, on := range []bool{o.reportPath != "", o.status, o.undo} {
		if on {
			modes++
		}
	}
	switch {
	case modes > 1 || (o.apply && (o.status || o.undo)):
		return errOneMode
	case o.apply && o.reportPath == "":
		return errApplyNoReport
	case modes == 0:
		return errNoMode
	}
	return nil
}

func maxPerRun(env func(string) string) (int, error) {
	raw := env("RELENGTH_MAX_PER_RUN")
	if raw == "" {
		return defaultMaxPerRun, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 {
		return 0, fmt.Errorf("RELENGTH_MAX_PER_RUN must be a positive integer, got %q", raw)
	}
	return value, nil
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
		fmt.Fprintf(os.Stderr, "libraryrelength: %v\n", err)
		os.Exit(1)
	}
}

func run(opts options, env func(string) string, out io.Writer) error {
	limit, err := maxPerRun(env)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()
	pool, err := openPool(ctx, env("RELENGTH_DATABASE_URL"), !opts.apply && !opts.undo)
	if err != nil {
		return err
	}
	defer pool.Close()
	return dispatch(ctx, opts, store{pool: pool, queue: persistence.NewPgxJobQueue(pool)}, limit, out)
}

func dispatch(ctx context.Context, opts options, s store, limit int, out io.Writer) error {
	switch {
	case opts.status:
		rows, err := loadStatus(ctx, s.pool)
		if err != nil {
			return err
		}
		writeStatus(out, rows)
		return nil
	case opts.undo:
		return runUndo(ctx, s, opts.undoRunID, out)
	}
	changes, err := loadPlan(opts.reportPath, opts.apply)
	if err != nil {
		return err
	}
	return runPlan(ctx, s, changes, limit, opts.apply, uuid.New(), out)
}

func loadPlan(path string, apply bool) ([]change, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open report: %w", err)
	}
	defer func() { _ = file.Close() }()
	if apply {
		return readApplyPlan(file)
	}
	return readPlan(file)
}
