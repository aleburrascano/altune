package main

import (
	"altune/go-api/internal/acquisition/adapters/chromaprint"
	"altune/go-api/internal/catalog/adapters/storage"
	"altune/go-api/internal/shared/binpath"
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
	"strings"
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
	deezerRateVarName      = "LIBRARYVERIFY_DEEZER_RATE"
	matchMaxVarName        = "LIBRARYVERIFY_PREVIEW_MATCH_MAX_BER"
	absentMinVarName       = "LIBRARYVERIFY_PREVIEW_ABSENT_MIN_BER"

	defaultConcurrency = 3
	maxConcurrency     = 16
	maxAcoustIDRate    = 3.0
	maxMusicBrainzRate = 1.0
	defaultMinScore    = chromaprint.MinScore
	maxMinScore        = 1.0

	defaultDeezerRate  = 1.0
	maxDeezerRate      = 5.0
	defaultMatchMaxBER = 0.18
	defaultAbsentMin   = 0.35
)

var (
	errEmptyTracksFile    = errors.New("-tracks file lists no track ids")
	errNegativeLimit      = errors.New("-limit must not be negative")
	errMissingAcoustIDKey = errors.New("ACOUSTID_API_KEY is not set")
	errFpcalcMissing      = errors.New("fpcalc is not runnable; run this inside the go-api image")
	errBadConcurrency     = fmt.Errorf("%s must be a whole number from 1 to %d", concurrencyVarName, maxConcurrency)
	errBadAcoustIDRate    = fmt.Errorf("%s must be a number above 0 and at most %g (AcoustID's limit)", acoustIDRateVarName, maxAcoustIDRate)
	errBadMusicBrainzRate = fmt.Errorf("%s must be a number above 0 and at most %g (MusicBrainz's limit)", musicBrainzRateVarName, maxMusicBrainzRate)
	errBadDeezerRate      = fmt.Errorf("%s must be a number above 0 and at most %g (Deezer's limit)", deezerRateVarName, maxDeezerRate)
	errBadPreviewCut      = fmt.Errorf("%s and %s must be bit error rates between 0 and 1, the match cut below the absent cut", matchMaxVarName, absentMinVarName)
	errBadMinScore        = fmt.Errorf("%s must be a number from %g (the score the app binds at) to %g", minScoreVarName, chromaprint.MinScore, maxMinScore)
)

type options struct {
	userID     string
	limit      int
	reportPath string
	tracksFile string
	calibrate  bool
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
	flags.StringVar(&opts.tracksFile, "tracks", "", "only the track ids listed in this file, one per line")
	flags.BoolVar(&opts.calibrate, "calibrate", false, "measure preview bit error rates on same-song and different-song pairs instead of verifying")
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

type previewSettings struct {
	deezerRate float64
	cut        previewCut
}

func previewSettingsFromEnv(env func(string) string) (previewSettings, error) {
	var err error
	s := previewSettings{deezerRate: defaultDeezerRate, cut: previewCut{matchMax: defaultMatchMaxBER, absentMin: defaultAbsentMin}}
	if s.deezerRate, err = floatFromEnv(env, deezerRateVarName, s.deezerRate, isRateUpTo(maxDeezerRate), errBadDeezerRate); err != nil {
		return previewSettings{}, err
	}
	if s.cut.matchMax, err = floatFromEnv(env, matchMaxVarName, s.cut.matchMax, isFraction, errBadPreviewCut); err != nil {
		return previewSettings{}, err
	}
	if s.cut.absentMin, err = floatFromEnv(env, absentMinVarName, s.cut.absentMin, isFraction, errBadPreviewCut); err != nil {
		return previewSettings{}, err
	}
	if s.cut.matchMax >= s.cut.absentMin {
		return previewSettings{}, errBadPreviewCut
	}
	return s, nil
}

func isFraction(value float64) bool { return value > 0 && value < 1 }

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
	tuning, previewTuning, err := tuningFromEnv(env)
	if err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	v, err := verifierFromConfig(cfg, tuning, previewTuning)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()
	tracks, err := readTracks(ctx, cfg.DatabaseURL, opts)
	if err != nil {
		return err
	}
	return verifyOrCalibrate(ctx, v, tracks, opts, tuning, out)
}

func verifyOrCalibrate(ctx context.Context, v *verifier, tracks []libraryTrack, opts options, tuning settings, out io.Writer) error {
	if opts.calibrate {
		return publishCalibration(v.calibrate(ctx, tracks), opts.reportPath, out)
	}
	v.checkpoint = checkpointTo(opts.reportPath, tuning.minScore, os.Stderr)
	return publish(buildReport(v.verifyAll(ctx, tracks, tuning.concurrency), tuning.minScore, time.Now()), opts.reportPath, out)
}

func tuningFromEnv(env func(string) string) (settings, previewSettings, error) {
	tuning, err := settingsFromEnv(env)
	if err != nil {
		return settings{}, previewSettings{}, err
	}
	previewTuning, err := previewSettingsFromEnv(env)
	return tuning, previewTuning, err
}

func publishCalibration(report calibrationReport, reportPath string, out io.Writer) error {
	if err := saveJSON(reportPath, report); err != nil {
		return err
	}
	writeCalibrationSummary(out, report, reportPath)
	return nil
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

func verifierFromConfig(cfg *config.Config, tuning settings, previewTuning previewSettings) (*verifier, error) {
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
	client := &http.Client{Timeout: httpTimeout}
	printer := fpcalcPrinter{binary: binpath.Resolve("fpcalc", cfg.FFmpegLocation)}
	return &verifier{
		store: audioStore, identify: identifier, minScore: tuning.minScore, progress: os.Stderr,
		lookups:  newMusicBrainz(client, musicBrainzBaseURL, tuning.musicBrainzRate),
		previews: newDeezerPreviews(client, deezerBaseURL, previewTuning.deezerRate, printer),
		printer:  printer, cut: previewTuning.cut,
	}, nil
}

func readTracks(ctx context.Context, databaseURL string, opts options) ([]libraryTrack, error) {
	trackIDs, err := readTrackIDs(opts.tracksFile)
	if err != nil {
		return nil, err
	}
	pool, err := openReadOnlyPool(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	defer pool.Close()
	return loadReadyTracks(ctx, pool, opts.userID, opts.limit, trackIDs)
}

func readTrackIDs(tracksFile string) ([]string, error) {
	if tracksFile == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(tracksFile)
	if err != nil {
		return nil, fmt.Errorf("read tracks file: %w", err)
	}
	ids := strings.Fields(string(raw))
	if len(ids) == 0 {
		return nil, errEmptyTracksFile
	}
	return ids, nil
}
