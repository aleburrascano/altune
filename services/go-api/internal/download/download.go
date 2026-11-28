package download

import (
	"altune/go-api/internal/download/internal/adapters/chromaprint"
	"altune/go-api/internal/download/internal/adapters/evidence"
	"altune/go-api/internal/download/internal/adapters/ffmpegtag"
	"altune/go-api/internal/download/internal/adapters/handler"
	"altune/go-api/internal/download/internal/adapters/id3"
	"altune/go-api/internal/download/internal/adapters/memory"
	"altune/go-api/internal/download/internal/adapters/persistence"
	"altune/go-api/internal/download/internal/adapters/previewcheck"
	"altune/go-api/internal/download/internal/adapters/qualitycheck"
	"altune/go-api/internal/download/internal/adapters/savedlink"
	"altune/go-api/internal/download/internal/adapters/scratchfiles"
	"altune/go-api/internal/download/internal/adapters/youtubemusic"
	"altune/go-api/internal/download/internal/adapters/ytdlp"
	"altune/go-api/internal/download/internal/adapters/ytmsearch"
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/download/internal/service"
	"altune/go-api/internal/platform/outbound"
	"altune/go-api/internal/shared/binpath"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/random"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

type (
	Status       = domain.SchedulerStatus
	Verification = domain.Verification
	JobRecord    = domain.JobRecord

	TrackLibrary    = ports.TrackLibrary
	StatusRules     = ports.StatusRules
	InPlaceVerdicts = ports.InPlaceVerdicts
	InPlaceRecord   = domain.InPlaceRecord
	TrackSnapshot   = domain.TrackSnapshot
	TrackStatus     = domain.TrackStatus
	TrackEvent      = domain.TrackEvent
	TrackLabel      = domain.TrackLabel
	CatalogSource   = domain.CatalogSource
	Completion      = domain.Completion
	ClaimNumber     = domain.ClaimNumber
	OrphanedAudio   = domain.OrphanedAudio

	FetchedAudio = domain.FetchedAudio

	RecordingResolverPorts = service.ResolverPorts
	RecordingResolverPort  = ports.RecordingResolver
	RecordingLookup        = ports.RecordingLookup
	RecordingSearch        = ports.RecordingSearch
	RecordingDetail        = domain.RecordingDetail
	RecordingRelease       = domain.RecordingRelease
	RecordingFacts         = domain.RecordingFacts
	WorkRecording          = domain.WorkRecording
	ISRCLookup             = ports.ISRCLookup
	ISRCRecording          = domain.ISRCRecording
	RerecordingLookup      = ports.RerecordingLookup
	RerecordingFinder      = ports.RerecordingFinder

	EvalCandidate     = qualitycheck.Candidate
	EvalCase          = qualitycheck.Case
	EvalCatalogSource = qualitycheck.CatalogSource
	EvalReport        = qualitycheck.Report
	EvalSource        = qualitycheck.Source
	EvalTrack         = qualitycheck.Track

	YouTubeMusicSearchClient = ytmsearch.Client
	YouTubeMusicSearchConfig = ytmsearch.Config
	YouTubeMusicSong         = ytmsearch.Song
	YouTubeMusicVideoType    = ytmsearch.VideoType
)

const (
	TrackPending = domain.TrackPending
	TrackReady   = domain.TrackReady
	TrackFailed  = domain.TrackFailed

	EventFailPending     = domain.EventFailPending
	EventMarkFailed      = domain.EventMarkFailed
	EventMarkReady       = domain.EventMarkReady
	EventReplaceAudio    = domain.EventReplaceAudio
	EventRevertToPending = domain.EventRevertToPending

	VideoTypeOfficialVideo = ytmsearch.VideoTypeOfficialVideo
	VideoTypeUserUpload    = ytmsearch.VideoTypeUserUpload
)

var (
	ErrTrackNotFound     = domain.ErrTrackNotFound
	ErrIllegalTransition = domain.ErrIllegalTransition
	ErrLeaseLost         = domain.ErrLeaseLost

	NewDatabase = persistence.NewDatabase

	NewRecordingResolver  = service.NewMusicBrainzResolver
	NoopISRCLookup        = memory.NoopISRCLookup
	NoopRecordingLookup   = memory.NoopRecordingLookup
	NoopRecordingResolver = memory.NoopRecordingResolver
	NoopRerecordingFinder = memory.NoopRerecordingFinder
	NoopRerecordingLookup = memory.NoopRerecordingLookup

	LoadEvalBaseline      = qualitycheck.LoadBaseline
	LoadEvalDir           = qualitycheck.LoadDir
	LoadEmbeddedEvalCases = qualitycheck.LoadEmbedded
	RunAllEvalCases       = qualitycheck.RunAll
	SummarizeEval         = qualitycheck.Summarize

	UnrequestedQualifiers = service.UnrequestedQualifiers
	IsYouTubeVideoID      = domain.IsYouTubeVideoID
	NewYtmSearchFor       = ytmsearch.ForClient
	YouTubeCanary         = ytdlp.YouTubeCanary
)

const (
	ytmSearchTimeout = 10 * time.Second
	acoustIDTimeout  = 15 * time.Second
)

var (
	errTransportRequired = errors.New("download: Deps.Transport is required, so acquisition calls never fall back to an unpaced transport")
	errDepsIncomplete    = errors.New("download: New needs a config, an events publisher, a client builder, the library half (Library), the orphaned-audio recorder, the stored-audio reader and the three recording ports")
	errVerdictsRequired  = errors.New("download: Deps.InPlaceVerdicts is required with a Database, so in-place verdicts are never dropped")
)

type Deps struct {
	Config    *config.Config
	Transport http.RoundTripper
	Database  *persistence.Database
	Redis     *goredis.Client
	Events    events.Publisher
	NewClient func(timeout time.Duration, transport http.RoundTripper) *http.Client

	Library         ports.TrackLibrary
	InPlaceVerdicts ports.InPlaceVerdicts
	AudioStore      ports.AudioWriter
	StoredAudio     ports.StoredAudioReader
	Orphans         ports.OrphanedAudioRecorder

	RecordingResolver ports.RecordingResolver
	RecordingLookup   ports.RecordingLookup
	Rerecordings      ports.RerecordingFinder

	Sources ports.SourceOrder
}

func (d Deps) validate() error {
	if d.Transport == nil {
		return errTransportRequired
	}
	if d.Config == nil || d.Events == nil || d.NewClient == nil || d.Library == nil || d.Orphans == nil || d.StoredAudio == nil ||
		d.RecordingResolver == nil || d.RecordingLookup == nil || d.Rerecordings == nil {
		return errDepsIncomplete
	}
	if d.Database != nil && d.InPlaceVerdicts == nil {
		return errVerdictsRequired
	}
	return nil
}

type Module struct {
	routes         handler.Routes
	scheduler      *service.BackgroundAcquisitionScheduler
	plan           func(context.Context) error
	planEvery      time.Duration
	checks         []service.Check
	signedOutSince func(context.Context) (time.Time, bool)
	outcomes       *persistence.PostgresOutcomeStore
	rejections     *persistence.PostgresRejectionStore
}

func (m *Module) Routes() handler.Routes { return m.routes }

func (m *Module) On() bool { return m.scheduler != nil }

func (m *Module) Scheduler() *service.BackgroundAcquisitionScheduler { return m.scheduler }

func (m *Module) Upgrade() (plan func(context.Context) error, every time.Duration) {
	return m.plan, m.planEvery
}

func (m *Module) HasSourceChecks() bool { return len(m.checks) > 0 }

func (m *Module) CheckSources(ctx context.Context) error {
	if err := service.RunChecks(ctx, clock.Now, m.checks); err != nil {
		return fmt.Errorf("%w", err)
	}
	return nil
}

func (m *Module) YouTubeSignedOutSince(ctx context.Context) (time.Time, bool) {
	return m.signedOutSince(ctx)
}

func (m *Module) Retention() (outcomes *persistence.PostgresOutcomeStore, rejections *persistence.PostgresRejectionStore) {
	return m.outcomes, m.rejections
}

func New(ctx context.Context, d Deps) (*Module, error) {
	if err := d.validate(); err != nil {
		return nil, err
	}
	service.SweepStaleTempDirs(ctx, scratchfiles.New(), clock.Now)

	inner, err := ytmsearch.ForClient(innerTubeClient(ctx, d.Config, d.Redis, d.Transport, d.NewClient))
	if err != nil {
		return nil, fmt.Errorf("build the ytmusic canary client: %w", err)
	}
	canarySearcher := YtDlpSearcher(d.Config)
	ytDlpInstalled := canarySearcher.Available()
	m := &Module{
		checks: sourceChecks(sourceProbes{
			ytDlp: canarySearcher, ytDlpAvailable: ytDlpInstalled, innerTube: inner,
			ytMusicEnabled: d.Config.YtMusicEnabled, ytDlpEnabled: d.Config.YtDLPEnabled,
		}),
		signedOutSince: canarySearcher.SignedOutSince,
		outcomes:       d.Database.Outcomes(),
		rejections:     d.Database.Rejections(),
	}
	if len(d.Sources.All()) == 0 || d.AudioStore == nil {
		return m, nil
	}
	return m.startAcquisition(ctx, d, domain.Verification{YtDlp: ytDlpInstalled})
}

func YtDlpSearcher(cfg *config.Config) *ytdlp.YouTubeAudioSearcher {
	return ytdlp.MustNewYtDlpAudioSearcher(cfg.FFmpegLocation, cfg.YtDLPCookieFile, cfg.YtDLPJSRuntime, clock.Now, clock.Sleep,
		ytdlp.WithRemoteComponents(cfg.YtDLPRemoteComponents),
		ytdlp.WithPOTokenURL(cfg.YtDLPPOTokenURL),
		ytdlp.WithEnvironment(cfg.Env))
}

func innerTubeClient(ctx context.Context, cfg *config.Config, redis *goredis.Client, transport http.RoundTripper, newClient func(time.Duration, http.RoundTripper) *http.Client) *http.Client {
	settings := outbound.YouTubeMusicBudgetSettings{MinInterval: cfg.YtmInnerTubeMinInterval, Cooldown: cfg.YtmInnerTubeCooldown}
	budget, err := outbound.NewYouTubeMusicBudget(redis, settings, clock.Now, clock.Sleep)
	if err != nil {
		slog.WarnContext(ctx, "acquisition: no shared InnerTube budget without redis, pacing through the shared outbound pacer only")
		return newClient(ytmSearchTimeout, transport)
	}
	return newClient(ytmSearchTimeout, budget.Transport(transport))
}

func LiveSources(ctx context.Context, cfg *config.Config, redis *goredis.Client, transport http.RoundTripper, newClient func(time.Duration, http.RoundTripper) *http.Client) (ports.SourceOrder, error) {
	searcher := YtDlpSearcher(cfg)
	var tiers ports.SourceOrder
	switch {
	case cfg.YtMusicEnabled:
		songs, err := ytmsearch.ForClient(innerTubeClient(ctx, cfg, redis, transport, newClient))
		if err != nil {
			return ports.SourceOrder{}, fmt.Errorf("enable audio sources: build the ytmusic search client: %w", err)
		}
		tiers.First = append(tiers.First, ytmsearch.NewSource(songs, searcher), youtubemusic.NewSource(searcher, songs))
	default:
		slog.InfoContext(ctx, "acquisition: ytmusic source disabled via YTMUSIC_ENABLED")
	}
	switch {
	case cfg.YtDLPEnabled:
		source := ytdlp.NewSource(searcher)
		tiers.Fallback = append(tiers.Fallback, source)
		tiers.Previewing = append(tiers.Previewing, source)
	default:
		slog.InfoContext(ctx, "acquisition: yt-dlp source disabled via YTDLP_ENABLED")
	}
	if len(tiers.All()) > 0 {
		tiers.First = append([]ports.AudioSource{savedlink.MustNewSource(searcher, savedlink.NewHTTPPageReader(transport, cfg.AcquisitionFileHostPageTimeout))}, tiers.First...)
	}
	return tiers, nil
}

type sourceProbes struct {
	ytDlp          *ytdlp.YouTubeAudioSearcher
	ytDlpAvailable bool
	innerTube      *ytmsearch.Client
	ytMusicEnabled bool
	ytDlpEnabled   bool
}

func sourceChecks(p sourceProbes) []service.Check {
	var checks []service.Check
	if p.ytDlp != nil && p.ytDlpAvailable {
		probe := func(source ytdlp.CanarySource) service.Check {
			return service.Check{Name: source.Name, Run: func(ctx context.Context) error { return p.ytDlp.Canary(ctx, source) }}
		}
		if p.ytDlpEnabled {
			checks = append(checks, probe(ytdlp.YouTubeCanary))
		}
		if p.ytMusicEnabled {
			checks = append(checks, probe(ytdlp.YTMusicCanary))
		}
		if p.ytDlpEnabled {
			checks = append(checks, probe(ytdlp.SoundCloudCanary))
		}
	}
	if p.ytMusicEnabled && p.innerTube != nil {
		checks = append(checks, service.Check{Name: string(ytmsearch.SourceName), Run: p.innerTube.Canary})
	}
	return checks
}

type acquisitionWorker struct {
	*service.AcquireTrackAudioService
	*service.ReplaceService
}

type acquireOption = func(*service.AcquireTrackAudioService)

func (m *Module) startAcquisition(ctx context.Context, d Deps, tools domain.Verification) (*Module, error) {
	cfg := d.Config
	sources := d.Sources
	upgradeState := ports.UpgradeState(memory.NewUpgradeState())
	if d.Database != nil {
		upgradeState = d.Database.UpgradeStore()
	}
	ledger, err := service.NewTrackLedger(d.Library, upgradeState)
	if err != nil {
		return nil, fmt.Errorf("build the track ledger: %w", err)
	}
	skips := &service.VerifySkipCounter{}
	prober := ytdlp.MustNewFfprobeProber(cfg.FFmpegLocation, skips)
	tools.Ffprobe, tools.Ffmpeg = prober.Available()
	health, identifier, storedAudio := fingerprinting(ctx, d, &tools)

	acquireSvc := service.MustNewAcquireTrackAudioService(
		ledger,
		service.NewSourceRegistryWithFallback(sources.First, sources.Fallback, sources.Previewing...),
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
			Tagger:        ffmpegtag.NewTagger(binpath.Resolve("ffmpeg", cfg.FFmpegLocation), id3.NewTagger()),
			Rejections:    d.Database.Rejections(),
			StoredAudio:   storedAudio,
			Orphans:       d.Orphans,
			Skips:         skips,
			Evidence:      evidence.NewCodec(),
			Events:        d.Events,
			DownloadLimit: service.MustLimitedTo(cfg.AcquisitionDownloadConcurrency),
			Observer:      service.NoObserver(),
			Sleep:         clock.Sleep,
			Now:           clock.Now,
			Unit:          random.Real,
		},
		[]acquireOption{
			service.WithSupersededAudioGrace(cfg.AcquisitionSupersededAudioGrace),
			service.WithAcquireStoreKeyPrefix(cfg.AudioKeyPrefix),
			service.WithAcquireConfidenceFloor(cfg.AcquisitionConfidenceFloor),
			service.WithProvenMaxBitErrorRate(cfg.AcquisitionUpgradePreviewAMaxBER),
		}...,
	)
	replaceSvc := service.NewReplaceService(acquireSvc)
	lane, err := upgradeLane(d, ledger, replaceSvc, service.NewInPlaceChecker(acquireSvc))
	if err != nil {
		return nil, err
	}
	m.scheduler = service.MustNewBackgroundAcquisitionScheduler(ctx, acquisitionWorker{acquireSvc, replaceSvc}, cfg.AcquisitionConcurrency, clock.Now,
		schedulerPorts(d, ledger, lane, skips, health, tools),
		service.WithSchedulerPaused(cfg.AcquisitionPaused),
		service.WithDrainBudget(time.Duration(cfg.AcquisitionDrainBudgetSeconds)*time.Second),
	)
	if d.Database == nil {
		return m, nil
	}
	m.routes = handler.NewRoutes(
		handler.NewRetryHandler(ledger, m.scheduler, service.NewRetryAdmission(d.Database.Cooldowns())),
		handler.NewReacquireHandler(ledger, m.scheduler, service.NewReacquireAdmission(d.Database.Cooldowns())),
	)
	m.plan = service.NewReplacementPlanner(d.Database.UpgradeStore(), m.scheduler, cfg.AcquisitionUpgradeConcurrency, cfg.AcquisitionUpgradeRetryAfter).
		CheckingUploads().UpgradingFrom(d.Library).RescuingLowBitrate(cfg.AcquisitionLowBitrateFloorKbps).PlanNext
	m.planEvery = service.PlanInterval(cfg.AcquisitionUpgradePerHour)
	return m, nil
}

func fingerprinting(ctx context.Context, d Deps, tools *domain.Verification) (*service.FingerprintHealth, ports.AudioIdentifier, ports.StoredAudioReader) {
	cfg := d.Config
	if cfg.AcoustIDAPIKey == "" {
		slog.WarnContext(ctx, "acquisition: fingerprint verification disabled", "reason", "ACOUSTID_API_KEY is empty")
		return service.NewFingerprintHealth(memory.NoopAudioIdentifier()), memory.NoopAudioIdentifier(), memory.NoopStoredAudioReader()
	}
	identifier := chromaprint.MustNewIdentifier(ctx, cfg.FFmpegLocation, cfg.AcoustIDAPIKey, d.NewClient(acoustIDTimeout, d.Transport))
	tools.Fpcalc = identifier.Available()
	health := service.NewFingerprintHealth(identifier)
	slog.InfoContext(ctx, "acquisition: fingerprint verification wired", "fpcalc", tools.Fpcalc)
	return health, health, d.StoredAudio
}

func upgradePreviewMatcher(d Deps) ports.PreviewMatcher {
	cfg := d.Config
	printer := previewcheck.NewFpcalcPrinter(binpath.Resolve("fpcalc", cfg.FFmpegLocation))
	previews := previewcheck.MustNew(d.NewClient(cfg.AcquisitionDeezerPreviewTimeout, d.Transport), previewcheck.BaseURL, printer, clock.Sleep, clock.Now)
	return previewcheck.NewMatcher(previews, printer, previewcheck.DefaultMatchMaxBitErrorRate)
}

func upgradeLane(d Deps, ledger ports.LibraryTracks, replace *service.ReplaceService, checker *service.InPlaceChecker) (service.UpgradeLane, error) {
	cfg := d.Config
	if d.Database == nil || cfg.AcquisitionUpgradeConcurrency < 1 {
		return service.NoUpgradeLane(), nil
	}
	store := d.Database.UpgradeStore()
	recorder, err := service.NewInPlaceRecorder(d.InPlaceVerdicts, store)
	if err != nil {
		return service.UpgradeLane{}, fmt.Errorf("build the in-place recorder: %w", err)
	}
	inPlace, err := service.InPlaceCheckOf(checker, recorder, cfg.AcquisitionUpgradeVerifyPerHour, clock.Now, clock.Sleep)
	if err != nil {
		return service.UpgradeLane{}, fmt.Errorf("build the in-place check: %w", err)
	}
	runner := service.MustNewRunner(ledger, replace, store, evidence.NewCodec(),
		service.RunnerConfig{
			MaxTries:            cfg.AcquisitionUpgradeMaxTries,
			LowBitrateFloorKbps: cfg.AcquisitionLowBitrateFloorKbps,
		}, inPlace)
	return service.MustUpgradeLaneOf(runner, cfg.AcquisitionUpgradeConcurrency), nil
}

func schedulerPorts(d Deps, ledger ports.LibraryTracks, lane service.UpgradeLane, skips *service.VerifySkipCounter, health *service.FingerprintHealth, tools domain.Verification) service.SchedulerPorts {
	wiring := service.SchedulerPorts{
		Queue:               memory.NewQueueFactory(),
		Upgrades:            lane,
		Notifier:            memory.NoopJobNotifier(),
		Outcomes:            memory.NoopOutcomeRecorder(),
		Events:              d.Events,
		VerifySkips:         skips.Count,
		FingerprintVerified: health.Verified,
		Timer:               clock.NewTimer,
		Ticker:              clock.NewTicker,
		Unit:                random.Real,
		Verification:        tools,
	}
	if d.Database != nil {
		wiring.Queue = service.QueueOf(d.Database.JobQueue(ledger))
		wiring.Notifier = d.Database.JobNotifier(clock.Sleep)
		wiring.Outcomes = d.Database.Outcomes()
	}
	return wiring
}
