package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"
)

const (
	runTimeout         = 5 * time.Minute
	databaseURLVarName = "LIBRARYHYGIENE_DATABASE_URL"
)

type options struct {
	reportPath string
}

func parseOptions(args []string, output io.Writer) (options, error) {
	flags := flag.NewFlagSet("libraryhygiene", flag.ContinueOnError)
	flags.SetOutput(output)
	var opts options
	flags.StringVar(&opts.reportPath, "report", "", "where to write the JSON report for libraryreconcile (default stdout)")
	if err := flags.Parse(args); err != nil {
		return options{}, err
	}
	return opts, nil
}

func main() {
	opts, err := parseOptions(os.Args[1:], os.Stderr)
	if errors.Is(err, flag.ErrHelp) {
		os.Exit(0)
	}
	if err == nil {
		err = run(opts, os.Getenv, os.Stdout, os.Stderr)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "libraryhygiene: %v\n", err)
		os.Exit(1)
	}
}

func run(opts options, env func(string) string, stdout, stderr io.Writer) error {
	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()
	pool, err := openReadOnlyPool(ctx, env(databaseURLVarName))
	if err != nil {
		return err
	}
	defer pool.Close()
	tracks, err := loadUnmatchedTracks(ctx, pool)
	if err != nil {
		return err
	}
	rep := buildReport(tracks, time.Now())
	if _, err := fmt.Fprintf(stderr, "libraryhygiene: %d unmatched tracks, %d fixes %v\n", rep.Tracks, len(rep.Findings), rep.Summary); err != nil {
		return err
	}
	return emit(opts, stdout, rep)
}

func emit(opts options, stdout io.Writer, rep report) error {
	if opts.reportPath == "" {
		return writeReport(stdout, rep)
	}
	file, err := os.Create(opts.reportPath)
	if err != nil {
		return fmt.Errorf("create report: %w", err)
	}
	if err := writeReport(file, rep); err != nil {
		_ = file.Close()
		return fmt.Errorf("write report: %w", err)
	}
	return file.Close()
}
