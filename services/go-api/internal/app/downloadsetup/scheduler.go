package downloadsetup

import (
	"altune/go-api/internal/app/bridges/downloadfromlibrary"
	"altune/go-api/internal/app/bridges/downloadfromsearch"
	"altune/go-api/internal/app/fingerprintstatus"
	"altune/go-api/internal/download"
	"altune/go-api/internal/library"
	"altune/go-api/internal/platform/monitoring"
	"altune/go-api/internal/platform/outbound"
	"altune/go-api/internal/shared/binpath"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/random"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Deps struct {
	Config       *config.Config
	Transport    http.RoundTripper
	Pool         *pgxpool.Pool
	DBMetrics    library.DBCallMetrics
	Events       *monitoring.EventTap
	Recording    downloadfromsearch.RecordingWiring
	Ledger       download.TrackLedgerPort
	AudioStore   library.AudioStore
	Sources      SourceOrder
	Verification download.Verification
}

type acquireOption = func(*download.AcquireTrackAudioService)

var ErrTransportRequired = errors.New("downloadsetup: Deps.Transport is required, so acquisition calls never fall back to an unpaced transport")

func NewScheduler(ctx context.Context, d Deps) *download.BackgroundAcquisitionScheduler {
	if d.Transport == nil {
		panic(ErrTransportRequired)
	}
	skips := &download.VerifySkipCounter{}
	prober := download.MustNewFfprobeProber(d.Config.FFmpegLocation, skips)
	d.Verification.Ffprobe, d.Verification.Ffmpeg = prober.Available()
	opts := storeOptions(d)
	lookups, identifier, storedAudio := fingerprinting(ctx, &d)
	opts = append(opts, pipelineOptions(d)...)
	acquireSvc := download.MustNewAcquireTrackAudioService(
		d.Ledger,
		download.NewSourceRegistry(d.Sources.First...).WithFallback(d.Sources.Fallback...),
		d.AudioStore,
		download.NewTempWorkspace(),
		download.VerificationPorts{
			Identifier:      identifier,
			RecordingLookup: d.Recording.Lookup,
			Rerecordings:    d.Recording.Rerecordings,
			Previews:        upgradePreviewMatcher(d.Config, d.Transport),
			Recordings:      d.Recording.Resolver,
		},
		download.StagePorts{
			Prober:        prober,
			Tagger:        download.NewFFmpegTagger(binpath.Resolve("ffmpeg", d.Config.FFmpegLocation), download.NewId3Tagger()),
			Rejections:    download.NewPostgresRejectionStore(d.Pool),
			StoredAudio:   storedAudio,
			Orphans:       downloadfromlibrary.NewOrphanedAudioRecorder(library.NewPostgresOrphanedAudioRepository(d.Pool, d.DBMetrics)),
			Skips:         skips,
			Events:        d.Events,
			DownloadLimit: download.MustLimitedTo(d.Config.AcquisitionDownloadConcurrency),
			Observer:      download.NoObserver(),
			Sleep:         clock.Sleep,
			Now:           clock.Now,
			Unit:          random.Real,
		},
		opts...,
	)
	replaceSvc := download.NewReplaceService(acquireSvc)
	return download.MustNewBackgroundAcquisitionScheduler(ctx, acquisitionWorker{acquireSvc, replaceSvc}, d.Config.AcquisitionConcurrency, clock.Now, schedulerPorts(d, acquireSvc, replaceSvc, skips, lookups), schedulerOptions(d)...)
}

type acquisitionWorker struct {
	*download.AcquireTrackAudioService
	*download.ReplaceService
}

func storeOptions(d Deps) []acquireOption {
	cfg := d.Config
	return []acquireOption{
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
	identifier := download.MustNewChromaprintIdentifier(ctx, cfg.FFmpegLocation, cfg.AcoustIDAPIKey, outbound.MustNewClient(acoustIDTimeout, d.Transport))
	d.Verification.Fpcalc = identifier.Available()
	lookups := fingerprintstatus.NewTracker(identifier)
	slog.InfoContext(ctx, "acquisition: fingerprint verification wired", "fpcalc", d.Verification.Fpcalc)
	return lookups, lookups, downloadfromlibrary.NewStoredAudioReader(d.AudioStore)
}

func pipelineOptions(d Deps) []acquireOption {
	cfg := d.Config
	return []acquireOption{
		download.WithAcquireConfidenceFloor(cfg.AcquisitionConfidenceFloor),
		download.WithProvenMaxBitErrorRate(cfg.AcquisitionUpgradePreviewAMaxBER),
	}
}

func schedulerPorts(d Deps, acquireSvc *download.AcquireTrackAudioService, replaceSvc *download.ReplaceService, skips *download.VerifySkipCounter, lookups *fingerprintstatus.HealthTracker) download.SchedulerPorts {
	wiring := download.SchedulerPorts{
		Queue:               download.InMemoryQueue(),
		Upgrades:            download.NoUpgradeLane(),
		Notifier:            download.NoopJobNotifier(),
		Outcomes:            download.NoopOutcomeRecorder(),
		Events:              d.Events,
		VerifySkips:         skips.Count,
		FingerprintVerified: lookups.Verified,
		Timer:               clock.NewTimer,
		Ticker:              clock.NewTicker,
		Unit:                random.Real,
		Verification:        d.Verification,
	}
	if d.Pool != nil {
		wiring.Queue = download.QueueOf(download.MustNewPostgresJobQueue(d.Pool, d.Ledger))
		wiring.Upgrades = upgradeLane(d.Config, d.Pool, d.DBMetrics, d.Ledger, acquireSvc, replaceSvc)
		wiring.Notifier = download.MustNewPostgresJobNotifier(d.Pool, clock.Sleep)
		wiring.Outcomes = download.NewPostgresOutcomeStore(d.Pool)
	}
	return wiring
}

func schedulerOptions(d Deps) []func(*download.BackgroundAcquisitionScheduler) {
	return []func(*download.BackgroundAcquisitionScheduler){
		download.WithSchedulerPaused(d.Config.AcquisitionPaused),
		download.WithDrainBudget(time.Duration(d.Config.AcquisitionDrainBudgetSeconds) * time.Second),
	}
}
