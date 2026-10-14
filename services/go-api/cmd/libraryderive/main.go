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
)

const (
	httpTimeout        = 20 * time.Second
	runTimeout         = 4 * time.Hour
	defaultReportPath  = "libraryderive-report.json"
	databaseURLVarName = "LIBRARYDERIVE_DATABASE_URL"
)

const usageNotice = "libraryderive: do not run this from the prod VM, Deezer rate-blocks the prod IP and the app shares it; run it from another host with a read-only LIBRARYDERIVE_DATABASE_URL. It never writes the database."

var errNegativeLimit = errors.New("-limit must not be negative")

type options struct {
	userID     string
	limit      int
	reportPath string
}

func parseOptions(args []string, output io.Writer) (options, error) {
	flags := flag.NewFlagSet("libraryderive", flag.ContinueOnError)
	flags.SetOutput(output)
	var opts options
	flags.StringVar(&opts.userID, "user", "", "only this user id")
	flags.IntVar(&opts.limit, "limit", 0, "stop after this many tracks (0 means all)")
	flags.StringVar(&opts.reportPath, "report", defaultReportPath, "where to write the JSON report")
	if err := flags.Parse(args); err != nil {
		return options{}, err
	}
	if opts.limit < 0 {
		return options{}, errNegativeLimit
	}
	return opts, nil
}

func main() {
	opts, err := parseOptions(os.Args[1:], os.Stderr)
	if errors.Is(err, flag.ErrHelp) {
		os.Exit(0)
	}
	if err == nil {
		err = run(opts, os.Getenv)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "libraryderive: %v\n", err)
		os.Exit(1)
	}
}

func run(opts options, env func(string) string) error {
	fmt.Fprintln(os.Stderr, usageNotice)
	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()
	pool, err := openReadOnlyPool(ctx, env(databaseURLVarName))
	if err != nil {
		return err
	}
	defer pool.Close()
	tracks, err := loadBoundTracks(ctx, pool, opts.userID, opts.limit)
	if err != nil {
		return err
	}
	httpClient := &http.Client{Timeout: httpTimeout}
	d := newDeriver(newMusicBrainz(httpClient, musicBrainzBaseURL, musicBrainzRequestsSec), offlineDeezer{})
	result := d.deriveAll(ctx, tracks)
	return saveReport(opts.reportPath, len(tracks), result)
}

func saveReport(path string, tracks int, result derivation) error {
	reason := ""
	if result.Aborted != nil {
		reason = result.Aborted.Error()
	}
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create report: %w", err)
	}
	if err := writeReport(file, buildReport(tracks, result.Findings, reason, time.Now())); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close report: %w", err)
	}
	return result.Aborted
}

type offlineDeezer struct{}

func (offlineDeezer) TrackByISRC(context.Context, string) (deezerTrack, error) {
	return deezerTrack{}, errCatalogUnknown
}

func (offlineDeezer) TrackByID(context.Context, string) (deezerTrack, error) {
	return deezerTrack{}, errCatalogUnknown
}

func (offlineDeezer) SearchAlbums(context.Context, string, string) ([]deezerAlbum, error) {
	return nil, errCatalogUnknown
}

func (offlineDeezer) Album(context.Context, string) (deezerAlbum, error) {
	return deezerAlbum{}, errCatalogUnknown
}
