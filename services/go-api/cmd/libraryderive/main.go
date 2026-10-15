package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"
)

const (
	httpTimeout        = 20 * time.Second
	runTimeout         = 4 * time.Hour
	defaultReportPath  = "libraryderive-report.json"
	databaseURLVarName = "LIBRARYDERIVE_DATABASE_URL"

	throttleAttemptsVarName = "LIBRARYDERIVE_THROTTLE_ATTEMPTS"
	throttleMaxWaitVarName  = "LIBRARYDERIVE_THROTTLE_MAX_WAIT"
	defaultThrottleAttempts = 10
	defaultThrottleMaxWait  = time.Minute
)

const usageNotice = "libraryderive: do not run this from the prod VM, Deezer rate-blocks the prod IP and the app shares it; run it from another host with a read-only LIBRARYDERIVE_DATABASE_URL. It never writes the database."

var (
	errBadThrottleAttempts = errors.New(throttleAttemptsVarName + " must be a whole number of at least 1")
	errBadThrottleMaxWait  = errors.New(throttleMaxWaitVarName + " must be a duration above zero, like 90s")
)

var errNegativeLimit = errors.New("-limit must not be negative")

type options struct {
	userID     string
	limit      int
	reportPath string
	since      time.Time
}

func parseOptions(args []string, output io.Writer) (options, error) {
	flags := flag.NewFlagSet("libraryderive", flag.ContinueOnError)
	flags.SetOutput(output)
	var opts options
	flags.StringVar(&opts.userID, "user", "", "only this user id")
	flags.IntVar(&opts.limit, "limit", 0, "stop after this many tracks (0 means all)")
	flags.StringVar(&opts.reportPath, "report", defaultReportPath, "where to write the JSON report")
	var rawSince string
	flags.StringVar(&rawSince, "since", "", "only tracks added, or whose identity was written, since this RFC3339 time")
	if err := flags.Parse(args); err != nil {
		return options{}, err
	}
	if opts.limit < 0 {
		return options{}, errNegativeLimit
	}
	if rawSince != "" {
		var err error
		if opts.since, err = time.Parse(time.RFC3339, rawSince); err != nil {
			return options{}, fmt.Errorf("-since: %w", err)
		}
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
	tracks, err := loadBoundTracksSince(ctx, pool, opts.userID, opts.limit, opts.since)
	if err != nil {
		return err
	}
	policy, err := throttlePolicyFromEnv(env)
	if err != nil {
		return err
	}
	httpClient := &http.Client{Timeout: httpTimeout}
	musicBrainz := newMusicBrainz(httpClient, musicBrainzBaseURL, musicBrainzRequestsSec)
	deezer := newDeezer(httpClient, deezerBaseURL, deezerRequestsPerSec)
	musicBrainz.client.withThrottle(policy)
	deezer.client.withThrottle(policy)
	d := newDeriver(musicBrainz, deezer)
	d.covers = newHTTPCoverProbe(httpClient, coverCDNRequestsPerSec)
	result := d.deriveAll(ctx, tracks)
	return saveReport(opts.reportPath, len(tracks), result)
}

func throttlePolicyFromEnv(env func(string) string) (throttlePolicy, error) {
	policy := throttlePolicy{Attempts: defaultThrottleAttempts, MaxWait: defaultThrottleMaxWait}
	if raw := env(throttleAttemptsVarName); raw != "" {
		attempts, err := strconv.Atoi(raw)
		if err != nil || attempts < 1 {
			return throttlePolicy{}, errBadThrottleAttempts
		}
		policy.Attempts = attempts
	}
	if raw := env(throttleMaxWaitVarName); raw != "" {
		wait, err := time.ParseDuration(raw)
		if err != nil || wait <= 0 {
			return throttlePolicy{}, errBadThrottleMaxWait
		}
		policy.MaxWait = wait
	}
	return policy, nil
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
