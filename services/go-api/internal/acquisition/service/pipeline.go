package service

import (
	acqdomain "altune/go-api/internal/acquisition/domain"
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/acquisition/service/judge"
	"altune/go-api/internal/acquisition/service/judgeshadow"
	"altune/go-api/internal/acquisition/service/upgrade"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/redact"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"time"
)

type (
	pipelineStart struct{}
	afterSearch   struct{}
	afterSelect   struct{}
	afterDownload struct{}
	afterTag      struct{}
	afterStore    struct{}
	afterUpdate   struct{}
)

type StepName string

const (
	stepNameSearch      StepName = "search"
	stepNameSelect      StepName = "select"
	stepNameDownload    StepName = "download"
	stepNameTag         StepName = "tag"
	stepNameStore       StepName = "store"
	stepNameUpdateTrack StepName = "update_track"
	stepNamePipeline    StepName = "pipeline"
)

type named interface {
	Name() StepName
}

type undoable interface {
	named
	Rollback(ctx context.Context, ac *AcquisitionContext) error
}

type stage[In, Out any] interface {
	undoable
	Execute(ctx context.Context, ac *AcquisitionContext, prev In) (Out, error)
}

type finalStage[In, Out any] interface {
	named
	Execute(ctx context.Context, ac *AcquisitionContext, prev In) (Out, error)
}

type Pipeline struct {
	search      stage[pipelineStart, afterSearch]
	selectBest  stage[afterSearch, afterSelect]
	download    stage[afterSelect, afterDownload]
	tag         stage[afterDownload, afterTag]
	store       stage[afterTag, afterStore]
	updateTrack finalStage[afterStore, afterUpdate]
}

func (p Pipeline) withUpdateTrack(s finalStage[afterStore, afterUpdate]) Pipeline {
	p.updateTrack = s
	return p
}

type StepError struct {
	Step StepName
	Err  error
}

func (e *StepError) Error() string { return fmt.Sprintf("step %s: %v", e.Step, e.Err) }
func (e *StepError) Unwrap() error { return e.Err }

type pipelineRun struct {
	ac        *AcquisitionContext
	reporter  jobReporter
	completed []undoable
	current   named
}

func RunPipeline(ctx context.Context, p Pipeline, ac *AcquisitionContext) (err error) {
	run := &pipelineRun{ac: ac, reporter: jobReporterFrom(ctx)}

	defer func() {
		if rec := recover(); rec != nil {
			step := stepNamePipeline
			if run.current != nil {
				step = run.current.Name()
			}
			slog.ErrorContext(ctx, "pipeline step panicked",
				"step", step, "track_id", ac.Track.ID, "panic", redact.LogText(fmt.Sprint(rec)), "stack", string(debug.Stack()))
			rollback(ctx, run.completed, ac)
			err = &StepError{Step: step, Err: fmt.Errorf("panic: %v", rec)}
		}
	}()

	downloaded, err := searchAndDownload(ctx, run, p)
	if err != nil && ac.FallbackSkipped && ctx.Err() == nil {
		slog.InfoContext(ctx, "acquisition.first_tier_failed_trying_fallback",
			"track_id", ac.Track.ID, "error", redact.LogError(err))
		ac.FallbackSkipped, ac.FallbackOnly = false, true
		ac.clearCandidates()
		run.completed = nil
		firstTierErr := err
		downloaded, err = searchAndDownload(ctx, run, p)
		err = keepFirstTierOutage(err, firstTierErr)
	}
	logShadowJudge(ctx, ac, err)
	if err != nil {
		return err
	}
	tagged, err := runStage(ctx, run, p.tag, downloaded)
	if err != nil {
		return err
	}
	stored, err := runStage(ctx, run, p.store, tagged)
	if err != nil {
		return err
	}
	if p.updateTrack == nil {
		return nil
	}
	_, err = execStage(ctx, run, p.updateTrack, stored)
	return err
}

func logShadowJudge(ctx context.Context, ac *AcquisitionContext, downloadErr error) {
	var kept *ports.AudioCandidate
	if downloadErr == nil {
		kept = ac.Selected
	}
	judgeshadow.Log(ctx, shadowTarget(ac.Track), ac.Track.ID, kept, ac.ProbedDuration, ac.Candidates)
}

func shadowTarget(t TrackRef) judge.Target {
	return judge.Target{Title: t.Title, Artist: t.Artist, Album: t.Album, Length: t.CatalogDuration, Explicit: t.CatalogExplicit}
}

func keepFirstTierOutage(fallbackErr, firstTierErr error) error {
	if fallbackErr == nil || acqdomain.IsSourceUnavailable(fallbackErr) || !acqdomain.IsSourceUnavailable(firstTierErr) {
		return fallbackErr
	}
	return fmt.Errorf("%w (first tier: %w)", fallbackErr, firstTierErr)
}

func searchAndDownload(ctx context.Context, run *pipelineRun, p Pipeline) (afterDownload, error) {
	searched, err := runStage(ctx, run, p.search, pipelineStart{})
	if err != nil {
		return afterDownload{}, err
	}
	selected, err := runStage(ctx, run, p.selectBest, searched)
	if err != nil {
		return afterDownload{}, err
	}
	return runStage(ctx, run, p.download, selected)
}

func runStage[In, Out any](ctx context.Context, run *pipelineRun, s stage[In, Out], prev In) (Out, error) {
	out, err := execStage(ctx, run, s, prev)
	if err != nil {
		return out, err
	}
	run.completed = append(run.completed, s)
	return out, nil
}

func announceStage(ctx context.Context, run *pipelineRun, s named) {
	ac := run.ac
	run.current = s
	run.reporter.stage(string(s.Name()))
	if ac.Selected != nil {
		run.reporter.source(ac.Selected.URL)
	}
	slog.InfoContext(ctx, "pipeline step starting", "step", s.Name(), "track_id", ac.Track.ID)
}

func execStage[In, Out any](ctx context.Context, run *pipelineRun, s finalStage[In, Out], prev In) (Out, error) {
	var none Out

	if ctxErr := ctx.Err(); ctxErr != nil {
		rollback(ctx, run.completed, run.ac)
		return none, fmt.Errorf("pipeline cancelled: %w", ctxErr)
	}

	announceStage(ctx, run, s)

	start := time.Now()
	out, execErr := s.Execute(ctx, run.ac, prev)
	timeStage(ctx, string(s.Name()), start)
	if execErr != nil {
		slog.ErrorContext(ctx, "pipeline step failed",
			"step", s.Name(), "track_id", run.ac.Track.ID, "error", redact.LogError(execErr))
		rollback(ctx, run.completed, run.ac)
		return none, &StepError{Step: s.Name(), Err: execErr}
	}

	return out, nil
}

const rollbackBudget = 30 * time.Second

type rollbackHandedOverKey struct{}

func withRollbackHandedOver(ctx context.Context) context.Context {
	return context.WithValue(ctx, rollbackHandedOverKey{}, true)
}

func rollbackHandedOver(ctx context.Context) bool {
	handedOver, _ := ctx.Value(rollbackHandedOverKey{}).(bool)
	return handedOver
}

func schedulerHandedOver(ctx context.Context) bool {
	ownedByScheduler, _ := ctx.Value(schedulerOwnedJobContextKey{}).(bool)
	return ownedByScheduler && errors.Is(ctx.Err(), context.Canceled)
}

func rollback(ctx context.Context, completed []undoable, ac *AcquisitionContext) {
	rbCtx, cancel := detachedTimeout(ctx, rollbackBudget)
	defer cancel()
	if schedulerHandedOver(ctx) {
		rbCtx = withRollbackHandedOver(rbCtx)
	}

	for i := len(completed) - 1; i >= 0; i-- {
		step := completed[i]
		slog.InfoContext(rbCtx, "rolling back step", "step", step.Name())
		if err := step.Rollback(rbCtx, ac); err != nil {
			slog.ErrorContext(rbCtx, "rollback failed", "step", step.Name(), "error", redact.LogError(err))
		}
	}
}

type AcquisitionContext struct {
	Track             TrackRef
	Identity          ports.RecordingIdentity
	Candidates        []ports.AudioCandidate
	SearchUnavailable error
	Ranked            []ports.AudioCandidate
	Selected          *ports.AudioCandidate
	TempPath          string
	TempDir           string
	AudioRef          string
	ProbedDuration    float64
	DurationVerified  bool
	IdentityVerified  bool
	Verdict           AudioVerdict
	Confidence        float64
	Evidence          Evidence
	BestEffort        bool
	SourceFormat      ports.SourceFormat

	Rejections []CandidateRejection
	Attempted  []AttemptedCandidate

	Replace ReplaceState

	FallbackSkipped bool
	FallbackOnly    bool

	PriorRejections []ports.PriorRejection

	Relabel *ports.RecordingLabel
}

func (ac *AcquisitionContext) clearCandidates() {
	ac.Candidates, ac.Ranked, ac.Selected = nil, nil, nil
}

func (ac *AcquisitionContext) retargetTo(label ports.RecordingLabel) {
	ac.clearCandidates()
	ac.FallbackSkipped, ac.FallbackOnly = false, false
	ac.Identity = ports.RecordingIdentity{}
	ac.Track.Title, ac.Track.Album, ac.Track.Year = label.Title, label.Album, label.Year
	ac.Track.ISRC, ac.Track.CatalogMBID, ac.Track.CatalogDuration = label.ISRC, label.MBID, label.DurationSeconds
	ac.Track.CatalogSources, ac.Track.AudioSourceURL = nil, ""
	ac.Relabel = &label
}

type AttemptedCandidate struct {
	URL    string
	Window int
}

type ReplaceState struct {
	ExcludeKeys    []string
	PreservedRef   string
	StoredAcoustID string
	Gate           upgrade.Gate

	StoredKnownWrong bool
}

func (r ReplaceState) allowsRerecording() bool {
	return r.PreservedRef == "" || r.StoredKnownWrong
}

func (r ReplaceState) excludes(url string) bool {
	key := sourceKey(url)
	for _, excluded := range r.ExcludeKeys {
		if excluded == key {
			return true
		}
	}
	return false
}

func (r ReplaceState) isCurrentFile(match ports.RecordingMatch) bool {
	return r.StoredAcoustID != "" && match.AcoustID == r.StoredAcoustID
}

func (r ReplaceState) excludesCandidate(c ports.AudioCandidate) bool {
	return r.excludes(c.URL)
}

func (ac *AcquisitionContext) priorRejected(url string) bool {
	key := sourceKey(url)
	for _, rejected := range ac.PriorRejections {
		if rejected.SourceKey == key && ac.stillJudgedOnTheSameBasis(rejected.Basis) {
			return true
		}
	}
	return false
}

func (ac *AcquisitionContext) stillJudgedOnTheSameBasis(recorded *ports.RejectionBasis) bool {
	if recorded == nil || recorded.ReferenceMBID != ac.Identity.MBID {
		return false
	}
	current := ac.ExpectedLength()
	if recorded.ExpectedSeconds <= 0 || current <= 0 {
		return recorded.ExpectedSeconds <= 0 && current <= 0
	}
	return durationWithinAuthoritativeTolerance(recorded.ExpectedSeconds, current)
}

func (ac *AcquisitionContext) Provenance() shared.AcquisitionProvenance {
	switch {
	case ac.BestEffort:
		return shared.ProvenanceBestEffort
	case ac.IdentityVerified:
		return shared.ProvenanceVerified
	case ac.DurationVerified && ac.lengthCorroborated():
		return shared.ProvenanceCorroborated
	default:
		return shared.ProvenanceBestEffort
	}
}

func (ac *AcquisitionContext) MeasuredDuration() float64 {
	if ac.ProbedDuration > 0 {
		return ac.ProbedDuration
	}
	if ac.Selected != nil {
		return ac.Selected.Duration
	}
	return 0
}

type TrackRef struct {
	ID          string
	UserID      string
	Title       string
	Artist      string
	Album       string
	ISRC        string
	Year        int
	TrackNumber int
	AlbumArtist string
	Genre       string

	CatalogDuration float64
	CatalogExplicit *bool
	CatalogMBID     string
	CatalogSources  []ports.CatalogSource

	AudioSourceURL string
}

func (s *AcquireTrackAudioService) buildSteps(userId shared.UserId, trackId shared.TrackId) Pipeline {
	return CoreSteps(s.sources, s.audioTagger, s.audioStore, s.audioProber, s.identifier, s.workspace,
		[]func(*DownloadStep){
			WithStepDownloadLimiter(s.downloadLimiter),
			WithRecordingLookup(s.recordingLookup),
			WithConfidenceFloor(s.confidenceFloor),
			WithStepVerifySkips(s.verifySkips),
			WithJudgeCuts(s.cuts),
		},
		WithStoreAudioRefGuard(s.ledger, trackId),
		WithStoreOrphanQueue(s.orphans, userId),
		WithStoreKeyPrefix(s.storeKeyPrefix)).
		withUpdateTrack(NewUpdateTrackStep(s.ledger, userId, trackId))
}

func CoreSteps(
	sources *SourceRegistry,
	tagger ports.AudioTagger,
	store ports.AudioWriter,
	prober ports.AudioProber,
	identifier ports.AudioIdentifier,
	workspace ports.TempWorkspace,
	downloadOpts []func(*DownloadStep),
	storeOpts ...func(*StoreStep),
) Pipeline {
	return Pipeline{
		search:     NewSearchStep(sources),
		selectBest: NewSelectStep(),
		download:   NewDownloadStep(sources, append([]func(*DownloadStep){WithDownloadProber(prober), WithDownloadIdentifier(identifier), WithVerifyWidth(prodVerifyWidth), WithStepTempWorkspace(workspace)}, downloadOpts...)...),
		tag:        NewTagStep(tagger),
		store:      NewStoreStep(store, append([]func(*StoreStep){WithStoreProber(prober), WithStoreWorkspace(workspace)}, storeOpts...)...),
	}
}
