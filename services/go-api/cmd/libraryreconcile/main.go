package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const runTimeout = 30 * time.Minute

var (
	errEmptyDatabaseURL = errors.New("RECONCILE_DATABASE_URL is not set")
	errNoMode           = errors.New("give -report <path> (dry run, or with -apply) or -undo <run_id>")
	errUndoWithReport   = errors.New("-undo cannot be combined with -report or -apply")
	errApplyNoReport    = errors.New("-apply needs -report <path>")
)

type options struct {
	reportPath string
	apply      bool
	undoRunID  uuid.UUID
	undo       bool
}

func parseOptions(args []string, output io.Writer) (options, error) {
	flags := flag.NewFlagSet("libraryreconcile", flag.ContinueOnError)
	flags.SetOutput(output)
	var opts options
	var rawRunID string
	flags.StringVar(&opts.reportPath, "report", "", "libraryaudit JSON report to reconcile from")
	flags.BoolVar(&opts.apply, "apply", false, "write the changes (default is a dry run)")
	flags.StringVar(&rawRunID, "undo", "", "restore the old values of this run id")
	if err := flags.Parse(args); err != nil {
		return options{}, err
	}
	if rawRunID != "" {
		return undoOptions(opts, rawRunID)
	}
	switch {
	case opts.reportPath == "" && opts.apply:
		return options{}, errApplyNoReport
	case opts.reportPath == "":
		return options{}, errNoMode
	}
	return opts, nil
}

func undoOptions(opts options, rawRunID string) (options, error) {
	runID, err := uuid.Parse(rawRunID)
	if err != nil {
		return options{}, fmt.Errorf("-undo: %w", err)
	}
	if opts.reportPath != "" || opts.apply {
		return options{}, errUndoWithReport
	}
	return options{undo: true, undoRunID: runID}, nil
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
		fmt.Fprintf(os.Stderr, "libraryreconcile: %v\n", err)
		os.Exit(1)
	}
}

func run(opts options, env func(string) string, out io.Writer) error {
	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()
	pool, err := openPool(ctx, env("RECONCILE_DATABASE_URL"))
	if err != nil {
		return err
	}
	defer pool.Close()
	access := pgx.ReadWrite
	if !opts.apply && !opts.undo {
		access = pgx.ReadOnly
	}
	if opts.undo {
		return runUndo(ctx, store{pool: pool, access: access}, opts.undoRunID, out)
	}
	plans, err := loadPlan(opts.reportPath)
	if err != nil {
		return err
	}
	return runReconcile(ctx, store{pool: pool, access: access}, plans, opts.apply, uuid.New(), out)
}

func loadPlan(path string) ([]trackPlan, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open report: %w", err)
	}
	defer func() { _ = file.Close() }()
	return readPlan(file, time.Now().Year())
}
