package audioacquisition

import (
	"altune/go-api/internal/acquisition"
	"altune/go-api/internal/app/acquisitioncatalog"
	"altune/go-api/internal/app/fingerprintstatus"
	"altune/go-api/internal/catalog"
	"altune/go-api/internal/observe"
	"altune/go-api/internal/shared/binpath"
	"altune/go-api/internal/shared/config"
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Deps struct {
	Config       *config.Config
	Pool         *pgxpool.Pool
	Events       *observe.EventTap
	Recording    []func(*acquisition.AcquireTrackAudioService)
	Ledger       acquisition.TrackLedgerPort
	AudioStore   catalog.AudioStore
	Sources      Tiers
	Verification acquisition.AcquisitionVerification
	Background   *sync.WaitGroup
	Slots        chan struct{}
}

type acquireOption = func(*acquisition.AcquireTrackAudioService)

func NewScheduler(ctx context.Context, d Deps) *acquisition.BackgroundAcquisitionScheduler {
	skips := &acquisition.VerifySkipCounter{}
	prober := acquisition.NewFfprobeProber(d.Config.FFmpegLocation, acquisition.WithSkipRecorder(skips))
	d.Verification.Ffprobe, d.Verification.Ffmpeg = prober.Available()
	opts := storeOptions(d, acquisition.WithAudioProber(prober), skips)
	opts = append(opts, d.Recording...)
	lookups, fingerprinting := fingerprintOptions(ctx, d.Config, &d.Verification)
	opts = append(opts, fingerprinting...)
	opts = append(opts, pipelineOptions(d)...)
	acquireSvc := acquisition.NewAcquireTrackAudioService(
		d.Ledger,
		acquisition.NewSourceRegistry(d.Sources.First...).WithFallback(d.Sources.Fallback...),
		d.AudioStore,
		opts...,
	)
	return acquisition.NewBackgroundAcquisitionScheduler(acquireSvc, d.Background, d.Slots, schedulerOptions(d, acquireSvc, skips, lookups)...)
}

func storeOptions(d Deps, probing acquireOption, skips *acquisition.VerifySkipCounter) []acquireOption {
	cfg := d.Config
	return []acquireOption{
		acquisition.WithAcquireEvents(d.Events),
		acquisition.WithAcquireOrphanQueue(acquisitioncatalog.NewOrphanedAudioRecorder(catalog.NewPgxOrphanedAudioRepository(d.Pool))),
		acquisition.WithSupersededAudioGrace(cfg.AcquisitionSupersededAudioGrace),
		probing,
		acquisition.WithVerifySkips(skips),
		acquisition.WithAudioTagger(acquisition.NewFFmpegTagger(binpath.Resolve("ffmpeg", cfg.FFmpegLocation), acquisition.NewId3Tagger())),
		acquisition.WithAcquireStoreKeyPrefix(cfg.AudioKeyPrefix),
		acquisition.WithRejectionStore(acquisition.NewPgxRejectionStore(d.Pool)),
	}
}

func fingerprintOptions(ctx context.Context, cfg *config.Config, verification *acquisition.AcquisitionVerification) (*fingerprintstatus.Tracker, []acquireOption) {
	if cfg.AcoustIDAPIKey == "" {
		slog.WarnContext(ctx, "acquisition: fingerprint verification disabled", "reason", "ACOUSTID_API_KEY is empty")
		return fingerprintstatus.NewTracker(nil), nil
	}
	identifier := acquisition.NewChromaprintIdentifier(cfg.FFmpegLocation, cfg.AcoustIDAPIKey)
	verification.Fpcalc = identifier.Available()
	lookups := fingerprintstatus.NewTracker(identifier)
	slog.InfoContext(ctx, "acquisition: fingerprint verification wired", "fpcalc", verification.Fpcalc)
	return lookups, []acquireOption{acquisition.WithAudioIdentifier(lookups)}
}

func pipelineOptions(d Deps) []acquireOption {
	cfg := d.Config
	return []acquireOption{
		acquisition.WithDownloadLimiter(acquisition.NewDownloadLimiter(cfg.AcquisitionDownloadConcurrency)),
		acquisition.WithAcquireConfidenceFloor(cfg.AcquisitionConfidenceFloor),
		acquisition.WithAcquireTempWorkspace(acquisition.NewTempWorkspace()),
		acquisition.WithStoredAudioReader(acquisitioncatalog.NewStoredAudioReader(d.AudioStore)),
		acquisition.WithPreviewMatcher(upgradePreviewMatcher(cfg), cfg.AcquisitionUpgradePreviewAMaxBER),
	}
}

func schedulerOptions(d Deps, acquireSvc *acquisition.AcquireTrackAudioService, skips *acquisition.VerifySkipCounter, lookups *fingerprintstatus.Tracker) []func(*acquisition.BackgroundAcquisitionScheduler) {
	opts := []func(*acquisition.BackgroundAcquisitionScheduler){
		acquisition.WithSchedulerPaused(d.Config.AcquisitionPaused),
		acquisition.WithSchedulerEvents(d.Events),
		acquisition.WithVerificationStatus(d.Verification),
		acquisition.WithVerifySkipCount(skips.Count),
		acquisition.WithFingerprintVerified(lookups.Verified),
		acquisition.WithDrainBudget(time.Duration(d.Config.AcquisitionDrainBudgetSeconds) * time.Second),
	}
	if d.Pool == nil {
		return opts
	}
	return append(opts,
		upgradeLane(d.Config, d.Pool, d.Ledger, acquireSvc),
		acquisition.WithJobQueue(acquisition.NewPgxJobQueue(d.Pool)),
		acquisition.WithJobNotifier(acquisition.NewPgxJobNotifier(d.Pool)),
		acquisition.WithOutcomeRecorder(acquisition.NewPgxOutcomeStore(d.Pool)))
}
