package main

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"
)

const (
	defaultRequestsPerSecond = 2.0
	defaultWorkers           = 2
	defaultReportPath        = "libraryaudit-report.json"
	deezerBaseURL            = "https://api.deezer.com"
	httpTimeout              = 20 * time.Second
	auditTimeout             = 2 * time.Hour
)

const usageNotice = "libraryaudit: do not run this from the prod VM, Deezer rate-blocks the prod IP and the app shares it; run it from another host with a read-only AUDIT_DATABASE_URL"

type settings struct {
	databaseURL       string
	userID            string
	limit             int
	requestsPerSecond float64
	workers           int
	reportPath        string
}

func loadSettings(env func(string) string) (settings, error) {
	cfg := settings{
		databaseURL: env("AUDIT_DATABASE_URL"),
		userID:      env("AUDIT_USER_ID"),
		reportPath:  cmp.Or(env("AUDIT_REPORT_PATH"), defaultReportPath),
	}
	var err error
	if cfg.requestsPerSecond, err = floatSetting(env, "AUDIT_DEEZER_RPS", defaultRequestsPerSecond); err != nil {
		return settings{}, err
	}
	if cfg.workers, err = intSetting(env, "AUDIT_WORKERS", defaultWorkers); err != nil {
		return settings{}, err
	}
	if cfg.limit, err = intSetting(env, "AUDIT_LIMIT", 0); err != nil {
		return settings{}, err
	}
	return cfg, nil
}

func floatSetting(env func(string) string, name string, fallback float64) (float64, error) {
	raw := env(name)
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive number, got %q", name, raw)
	}
	return value, nil
}

func intSetting(env func(string) string, name string, fallback int) (int, error) {
	raw := env(name)
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return 0, fmt.Errorf("%s must be a non-negative integer, got %q", name, raw)
	}
	return value, nil
}

func main() {
	if err := run(os.Getenv); err != nil {
		fmt.Fprintf(os.Stderr, "libraryaudit: %v\n", err)
		os.Exit(1)
	}
}

func run(env func(string) string) error {
	fmt.Fprintln(os.Stderr, usageNotice)
	cfg, err := loadSettings(env)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), auditTimeout)
	defer cancel()

	tracks, err := readTracks(ctx, cfg)
	if err != nil {
		return err
	}
	result := buildReport(auditLibrary(ctx, cfg, tracks), time.Now())
	if err := saveReport(cfg.reportPath, result); err != nil {
		return err
	}
	if result.Aborted {
		return fmt.Errorf("aborted: %s", result.AbortReason)
	}
	return nil
}

func auditLibrary(ctx context.Context, cfg settings, tracks []libraryTrack) auditOutcome {
	httpClient := &http.Client{Timeout: httpTimeout}
	cat := combinedCatalog{
		deezerClient:      newDeezerClient(httpClient, deezerBaseURL, cfg.requestsPerSecond),
		musicBrainzClient: newMusicBrainzClient(httpClient, musicBrainzBaseURL, musicBrainzRequestsPerSecond),
	}
	outcome := auditAll(ctx, cat, newCoverComparer(httpClient), tracks, cfg.workers)
	if outcome.AbortReason != "" {
		return outcome
	}
	triaged, err := triageLengthFindings(ctx, cat, cat.musicBrainzClient, tracks, outcome.Findings)
	outcome.Findings = triaged
	if err != nil {
		outcome.AbortReason = err.Error()
	}
	return outcome
}

type combinedCatalog struct {
	*deezerClient
	*musicBrainzClient
}

func readTracks(ctx context.Context, cfg settings) ([]libraryTrack, error) {
	pool, err := openReadOnlyPool(ctx, cfg.databaseURL)
	if err != nil {
		return nil, err
	}
	defer pool.Close()
	return loadReadyTracks(ctx, pool, cfg.userID, cfg.limit)
}

func saveReport(path string, result report) error {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create report: %w", err)
	}
	defer func() { _ = file.Close() }()
	if err := writeJSON(file, result); err != nil {
		return err
	}
	writeSummary(os.Stdout, result)
	fmt.Printf("report written to %s\n", path)
	return nil
}
