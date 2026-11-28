package download

import (
	"altune/go-api/internal/download/internal/adapters/chromaprint"
	"altune/go-api/internal/download/internal/adapters/ffmpegtag"
	"altune/go-api/internal/download/internal/adapters/id3"
	"altune/go-api/internal/download/internal/adapters/scratchfiles"
	"altune/go-api/internal/download/internal/adapters/ytdlp"
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/download/internal/service"
	"altune/go-api/internal/shared/binpath"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/random"
	"context"
	"log/slog"
	"time"
)

type acquireOption = func(*service.AcquireTrackAudioService)

func New(ctx context.Context, d Deps) *service.BackgroundAcquisitionScheduler {
	if d.Transport == nil {
		panic(ErrTransportRequired)
	}
	skips := &service.VerifySkipCounter{}
	prober := ytdlp.MustNewFfprobeProber(d.Config.FFmpegLocation, skips)
	d.Verification.Ffprobe, d.Verification.Ffmpeg = prober.Available()
	opts := storeOptions(d)
	lookups, identifier, storedAudio := fingerprinting(ctx, &d)
	opts = append(opts, pipelineOptions(d)...)
	acquireSvc := service.MustNewAcquireTrackAudioService(
		d.Ledger,
		service.NewSourceRegistryWithFallback(d.Sources.First, d.Sources.Fallback, d.Sources.Previewing...),
		d.AudioStore,
		scratchfiles.New(),
		service.VerificationPorts{
			Identifier:      identifier,
			RecordingLookup: d.RecordingLookup,
			Rerecordings:    d.Rerecordings,
			Previews:        upgradePreviewMatcher(d),
			Recordings:      d.RecordingResolver,
		},
		service.StagePorts{
			Prober:        prober,
			Tagger:        ffmpegtag.NewTagger(binpath.Resolve("ffmpeg", d.Config.FFmpegLocation), id3.NewTagger()),
			Rejections:    d.Database.Rejections(),
			StoredAudio:   storedAudio,
			Orphans:       d.Orphans,
			Skips:         skips,
			Events:        d.Events,
			DownloadLimit: service.MustLimitedTo(d.Config.AcquisitionDownloadConcurrency),
			Observer:      service.NoObserver(),
			Sleep:         clock.Sleep,
			Now:           clock.Now,
			Unit:          random.Real,
		},
		opts...,
	)
	replaceSvc := service.NewReplaceService(acquireSvc)
	return service.MustNewBackgroundAcquisitionScheduler(ctx, acquisitionWorker{acquireSvc, replaceSvc}, d.Config.AcquisitionConcurrency, clock.Now, schedulerPorts(d, replaceSvc, service.NewInPlaceChecker(acquireSvc), skips, lookups), schedulerOptions(d)...)
}

type acquisitionWorker struct {
	*service.AcquireTrackAudioService
	*service.ReplaceService
}

func storeOptions(d Deps) []acquireOption {
	cfg := d.Config
	return []acquireOption{
		service.WithSupersededAudioGrace(cfg.AcquisitionSupersededAudioGrace),
		service.WithAcquireStoreKeyPrefix(cfg.AudioKeyPrefix),
	}
}

func fingerprinting(ctx context.Context, d *Deps) (IdentifierHealth, ports.AudioIdentifier, ports.StoredAudioReader) {
	cfg := d.Config
	if cfg.AcoustIDAPIKey == "" {
		slog.WarnContext(ctx, "acquisition: fingerprint verification disabled", "reason", "ACOUSTID_API_KEY is empty")
		return d.TrackIdentity(nil), ports.NoopAudioIdentifier(), ports.NoopStoredAudioReader()
	}
	identifier := chromaprint.MustNewIdentifier(ctx, cfg.FFmpegLocation, cfg.AcoustIDAPIKey, d.NewClient(acoustIDTimeout, d.Transport))
	d.Verification.Fpcalc = identifier.Available()
	lookups := d.TrackIdentity(identifier)
	slog.InfoContext(ctx, "acquisition: fingerprint verification wired", "fpcalc", d.Verification.Fpcalc)
	return lookups, lookups, d.StoredAudio
}

func pipelineOptions(d Deps) []acquireOption {
	cfg := d.Config
	return []acquireOption{
		service.WithAcquireConfidenceFloor(cfg.AcquisitionConfidenceFloor),
		service.WithProvenMaxBitErrorRate(cfg.AcquisitionUpgradePreviewAMaxBER),
	}
}

func schedulerPorts(d Deps, replaceSvc *service.ReplaceService, checker *service.InPlaceChecker, skips *service.VerifySkipCounter, lookups IdentifierHealth) service.SchedulerPorts {
	wiring := service.SchedulerPorts{
		Queue:               service.InMemoryQueue(),
		Upgrades:            service.NoUpgradeLane(),
		Notifier:            ports.NoopJobNotifier(),
		Outcomes:            ports.NoopOutcomeRecorder(),
		Events:              d.Events,
		VerifySkips:         skips.Count,
		FingerprintVerified: lookups.Verified,
		Timer:               clock.NewTimer,
		Ticker:              clock.NewTicker,
		Unit:                random.Real,
		Verification:        d.Verification,
	}
	if d.Database != nil {
		wiring.Queue = service.QueueOf(d.Database.JobQueue(d.Ledger))
		wiring.Upgrades = buildUpgradeLane(d, replaceSvc, checker)
		wiring.Notifier = d.Database.JobNotifier(clock.Sleep)
		wiring.Outcomes = d.Database.Outcomes()
	}
	return wiring
}

func schedulerOptions(d Deps) []func(*service.BackgroundAcquisitionScheduler) {
	return []func(*service.BackgroundAcquisitionScheduler){
		service.WithSchedulerPaused(d.Config.AcquisitionPaused),
		service.WithDrainBudget(time.Duration(d.Config.AcquisitionDrainBudgetSeconds) * time.Second),
	}
}
