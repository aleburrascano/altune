package downloadsetup

import (
	"altune/go-api/internal/app/bridges/downloadfromlibrary"
	"altune/go-api/internal/app/bridges/downloadfromsearch"
	"altune/go-api/internal/app/fingerprintstatus"
	"altune/go-api/internal/download"
	"altune/go-api/internal/library"
	"altune/go-api/internal/platform/monitoring"
	"altune/go-api/internal/shared/binpath"
	"altune/go-api/internal/shared/config"
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Deps struct {
	Config       *config.Config
	Pool         *pgxpool.Pool
	Events       *monitoring.EventTap
	Recording    downloadfromsearch.RecordingWiring
	Ledger       download.TrackLedgerPort
	AudioStore   library.AudioStore
	Sources      SourceOrder
	Verification download.Verification
}

type acquireOption = func(*download.AcquireTrackAudioService)

func NewScheduler(ctx context.Context, d Deps) *download.BackgroundAcquisitionScheduler {
	skips := &download.VerifySkipCounter{}
	prober := download.NewFfprobeProber(d.Config.FFmpegLocation, download.WithSkipRecorder(skips))
	d.Verification.Ffprobe, d.Verification.Ffmpeg = prober.Available()
	opts := storeOptions(d)
	lookups, identifier, storedAudio := fingerprinting(ctx, &d)
	opts = append(opts, pipelineOptions(d)...)
	acquireSvc := download.NewAcquireTrackAudioService(
		d.Ledger,
		download.NewSourceRegistry(d.Sources.First...).WithFallback(d.Sources.Fallback...),
		d.AudioStore,
		download.NewTempWorkspace(),
		download.VerificationPorts{
			Identifier:      identifier,
			RecordingLookup: d.Recording.Lookup,
			Rerecordings:    d.Recording.Rerecordings,
			Previews:        upgradePreviewMatcher(d.Config),
			Recordings:      d.Recording.Resolver,
		},
		download.StagePorts{
			Prober:      prober,
			Tagger:      download.NewFFmpegTagger(binpath.Resolve("ffmpeg", d.Config.FFmpegLocation), download.NewId3Tagger()),
			Rejections:  download.NewPostgresRejectionStore(d.Pool),
			StoredAudio: storedAudio,
			Orphans:     downloadfromlibrary.NewOrphanedAudioRecorder(library.NewPostgresOrphanedAudioRepository(d.Pool)),
			Skips:       skips,
		},
		opts...,
	)
	return download.NewBackgroundAcquisitionScheduler(acquireSvc, d.Config.AcquisitionConcurrency, schedulerOptions(d, acquireSvc, skips, lookups)...)
}

func storeOptions(d Deps) []acquireOption {
	cfg := d.Config
	return []acquireOption{
		download.WithAcquireEvents(d.Events),
		download.WithSupersededAudioGrace(cfg.AcquisitionSupersededAudioGrace),
		download.WithAcquireStoreKeyPrefix(cfg.AudioKeyPrefix),
	}
}

func fingerprinting(ctx context.Context, d *Deps) (*fingerprintstatus.HealthTracker, download.AudioIdentifier, download.StoredAudioReader) {
	cfg := d.Config
	if cfg.AcoustIDAPIKey == "" {
		slog.WarnContext(ctx, "acquisition: fingerprint verification disabled", "reason", "ACOUSTID_API_KEY is empty")
		return fingerprintstatus.NewTracker(nil), download.NoopAudioIdentifier(), download.NoopStoredAudioReader()
	}
	identifier := download.NewChromaprintIdentifier(cfg.FFmpegLocation, cfg.AcoustIDAPIKey)
	d.Verification.Fpcalc = identifier.Available()
	lookups := fingerprintstatus.NewTracker(identifier)
	slog.InfoContext(ctx, "acquisition: fingerprint verification wired", "fpcalc", d.Verification.Fpcalc)
	return lookups, lookups, downloadfromlibrary.NewStoredAudioReader(d.AudioStore)
}

func pipelineOptions(d Deps) []acquireOption {
	cfg := d.Config
	return []acquireOption{
		download.WithDownloadLimiter(download.NewDownloadLimiter(cfg.AcquisitionDownloadConcurrency)),
		download.WithAcquireConfidenceFloor(cfg.AcquisitionConfidenceFloor),
		download.WithProvenMaxBitErrorRate(cfg.AcquisitionUpgradePreviewAMaxBER),
	}
}

func schedulerOptions(d Deps, acquireSvc *download.AcquireTrackAudioService, skips *download.VerifySkipCounter, lookups *fingerprintstatus.HealthTracker) []func(*download.BackgroundAcquisitionScheduler) {
	opts := []func(*download.BackgroundAcquisitionScheduler){
		download.WithSchedulerPaused(d.Config.AcquisitionPaused),
		download.WithSchedulerEvents(d.Events),
		download.WithVerificationStatus(d.Verification),
		download.WithVerifySkipCount(skips.Count),
		download.WithFingerprintVerified(lookups.Verified),
		download.WithDrainBudget(time.Duration(d.Config.AcquisitionDrainBudgetSeconds) * time.Second),
	}
	if d.Pool == nil {
		return opts
	}
	return append(opts,
		upgradeLane(d.Config, d.Pool, d.Ledger, acquireSvc),
		download.WithJobQueue(download.NewPostgresJobQueue(d.Pool)),
		download.WithJobNotifier(download.NewPostgresJobNotifier(d.Pool)),
		download.WithOutcomeRecorder(download.NewPostgresOutcomeStore(d.Pool)))
}
