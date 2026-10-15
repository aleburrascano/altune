package main

import (
	"altune/go-api/internal/acquisition/adapters/chromaprint"
	"altune/go-api/internal/catalog/adapters/storage"
	"altune/go-api/internal/shared/config"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"time"
)

const (
	runTimeout        = 12 * time.Hour
	httpTimeout       = 20 * time.Second
	defaultReportPath = "libraryverify-report.json"

	concurrencyVarName     = "LIBRARYVERIFY_CONCURRENCY"
	acoustIDRateVarName    = "LIBRARYVERIFY_ACOUSTID_RATE"
	musicBrainzRateVarName = "LIBRARYVERIFY_MUSICBRAINZ_RATE"
	minScoreVarName        = "LIBRARYVERIFY_MIN_SCORE"

	defaultConcurrency = 3
	maxConcurrency     = 16
	maxAcoustIDRate    = 3.0
	maxMusicBrainzRate = 1.0
	defaultMinScore    = chromaprint.MinScore
	maxMinScore        = 1.0
)

var (
	errNegativeLimit      = errors.New("-limit must not be negative")
	errMissingAcoustIDKey = errors.New("ACOUSTID_API_KEY is not set")
	errFpcalcMissing      = errors.New("fpcalc is not runnable; run this inside the go-api image")
	errBadConcurrency     = fmt.Errorf("%s must be a whole number from 1 to %d", concurrencyVarName, maxConcurrency)
	errBadAcoustIDRate    = fmt.Errorf("%s must be a number above 0 and at most %g (AcoustID's limit)", acoustIDRateVarName, maxAcoustIDRate)
	errBadMusicBrainzRate = fmt.Errorf("%s must be a number above 0 and at most %g (MusicBrainz's limit)", musicBrainzRateVarName, maxMusicBrainzRate)
	errBadMinScore        = fmt.Errorf("%s must be a number from %g (the score the app binds at) to %g", minScoreVarName, chromaprint.MinScore, maxMinScore)
)

type options struct {
	userID     string
	limit      int
	reportPath string
}

type settings struct {
	concurrency     int
	acoustIDRate    float64
	musicBrainzRate float64
	minScore        float64
}

func parseOptions(args []string, output io.Writer) (options, error) {
	flags := flag.NewFlagSet("libraryverify", flag.ContinueOnError)
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

func settingsFromEnv(env func(string) string) (settings, error) {
	s := settings{concurrency: defaultConcurrency, acoustIDRate: maxAcoustIDRate, musicBrainzRate: maxMusicBrainzRate, minScore: defaultMinScore}
	if raw := env(concurrencyVarName); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > maxConcurrency {
			return settings{}, errBadConcurrency
		}
		s.concurrency = n
	}
	var err error
	if s.acoustIDRate, err = floatFromEnv(env, acoustIDRateVarName, s.acoustIDRate, isRateUpTo(maxAcoustIDRate), errBadAcoustIDRate); err != nil {
		return settings{}, err
	}
	if s.musicBrainzRate, err = floatFromEnv(env, musicBrainzRateVarName, s.musicBrainzRate, isRateUpTo(maxMusicBrainzRate), errBadMusicBrainzRate); err != nil {
		return settings{}, err
	}
	if s.minScore, err = floatFromEnv(env, minScoreVarName, s.minScore, isScore, errBadMinScore); err != nil {
		return settings{}, err
	}
	return s, nil
}

func floatFromEnv(env func(string) string, name string, fallback float64, valid func(float64) bool, bad error) (float64, error) {
	raw := env(name)
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || !valid(value) {
		return 0, bad
	}
	return value, nil
}

func isRateUpTo(ceiling float64) func(float64) bool {
	return func(value float64) bool { return value > 0 && value <= ceiling }
}

func isScore(value float64) bool { return value >= chromaprint.MinScore && value <= maxMinScore }

func main() {
	opts, err := parseOptions(os.Args[1:], os.Stderr)
	if errors.Is(err, flag.ErrHelp) {
		os.Exit(0)
	}
	if err == nil {
		err = run(opts, os.Getenv, os.Stdout)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "libraryverify: %v\n", err)
		os.Exit(1)
	}
}

func run(opts options, env func(string) string, out io.Writer) error {
	tuning, err := settingsFromEnv(env)
	if err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	v, err := verifierFromConfig(cfg, tuning)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()
	tracks, err := readTracks(ctx, cfg.DatabaseURL, opts)
	if err != nil {
		return err
	}
	report := buildReport(v.verifyAll(ctx, tracks, tuning.concurrency), tuning.minScore, time.Now())
	return publish(report, opts.reportPath, out)
}

func publish(report verifyReport, reportPath string, out io.Writer) error {
	if err := saveReport(reportPath, report); err != nil {
		return err
	}
	writeSummary(out, report, reportPath)
	if failed := report.Counts[verdictFailed]; failed > 0 {
		return fmt.Errorf("%d tracks could not be verified", failed)
	}
	return nil
}

func verifierFromConfig(cfg *config.Config, tuning settings) (*verifier, error) {
	if cfg.AcoustIDAPIKey == "" {
		return nil, errMissingAcoustIDKey
	}
	audioStore, err := storage.FromConfig(cfg, slog.Default())
	if err != nil {
		return nil, err
	}
	identifier := chromaprint.NewIdentifier(cfg.FFmpegLocation, cfg.AcoustIDAPIKey).WithRequestsPerSecond(tuning.acoustIDRate)
	if !identifier.Available() {
		return nil, errFpcalcMissing
	}
	return &verifier{
		store:    audioStore,
		identify: identifier,
		lookups:  newMusicBrainz(&http.Client{Timeout: httpTimeout}, musicBrainzBaseURL, tuning.musicBrainzRate),
		minScore: tuning.minScore,
		progress: os.Stderr,
	}, nil
}

func readTracks(ctx context.Context, databaseURL string, opts options) ([]libraryTrack, error) {
	pool, err := openReadOnlyPool(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	defer pool.Close()
	return loadReadyTracks(ctx, pool, opts.userID, opts.limit)
}
