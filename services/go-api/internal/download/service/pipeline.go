package service

import (
	"altune/go-api/internal/download/internal/core/audioquality"
	acqdomain "altune/go-api/internal/download/internal/core/domain"
	"altune/go-api/internal/download/internal/core/judgeshadow"
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/download/internal/core/rejectedcandidates"
	"altune/go-api/internal/download/internal/core/titlematch"
	"altune/go-api/internal/download/internal/core/tracklength"
	"altune/go-api/internal/download/service/replacement"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/downloadprovenance"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/random"
	"altune/go-api/internal/shared/redact"
	sharedtracklength "altune/go-api/internal/shared/tracklength"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"
)

type (
	PipelineStart struct{}
	afterSearch   struct{}
	AfterSelect   struct{}
	afterDownload struct{ Kept ports.AudioCandidate }
	afterTag      struct{ Kept ports.AudioCandidate }
	AfterStore    struct{ Kept ports.AudioCandidate }
	afterUpdate   struct{}
)

type StepName string

const (
	stepNameSearch      StepName = "search"
	stepNameSelect      StepName = "select"
	StepNameDownload    StepName = "download"
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
	search     stage[PipelineStart, afterSearch]
	selectBest stage[afterSearch, AfterSelect]
	download   stage[AfterSelect, afterDownload]
	tag        stage[afterDownload, afterTag]
	store      stage[afterTag, AfterStore]
	finish     func(ctx context.Context, run *pipelineRun, stored AfterStore) error
	now        clock.Func
}

func endsAtStore(context.Context, *pipelineRun, AfterStore) error { return nil }

func (p Pipeline) withUpdateTrack(s finalStage[AfterStore, afterUpdate]) Pipeline {
	p.finish = func(ctx context.Context, run *pipelineRun, stored AfterStore) error {
		_, err := execStage(ctx, run, s, stored)
		return err
	}
	return p
}

type pendingStep struct{}

func (pendingStep) Name() StepName { return stepNamePipeline }

type StepError struct {
	Step StepName
	Err  error
}

func (e *StepError) Error() string { return fmt.Sprintf("step %s: %v", e.Step, e.Err) }
func (e *StepError) Unwrap() error { return e.Err }

type pipelineRun struct {
	ac        *AcquisitionContext
	completed []undoable
	current   named
	now       clock.Func
}

func RunPipeline(ctx context.Context, p Pipeline, ac *AcquisitionContext) error {
	run := &pipelineRun{ac: ac, current: pendingStep{}, now: p.now}
	var err error
	panicked := background.SafeRun(ctx, "acquisition.pipeline_panic", func() { err = runStages(ctx, run, p) },
		"step", run, logging.KeyTrackID, ac.Track.ID)
	if panicked == nil {
		return err
	}
	rollback(ctx, run.completed, ac)
	return &StepError{Step: run.currentStep(), Err: panicked}
}

func (run *pipelineRun) currentStep() StepName {
	return run.current.Name()
}

func (run *pipelineRun) LogValue() slog.Value {
	return slog.StringValue(string(run.currentStep()))
}

func runStages(ctx context.Context, run *pipelineRun, p Pipeline) error {
	downloaded, err := searchAndDownloadAnyTier(ctx, run, p)
	logShadowJudge(ctx, run.ac, err)
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
	return p.finish(ctx, run, stored)
}

func logShadowJudge(ctx context.Context, ac *AcquisitionContext, downloadErr error) {
	defer background.RecoverContext(ctx, "acquisition.judge_shadow_panic", logging.KeyTrackID, ac.Track.ID)
	var kept *ports.AudioCandidate
	if downloadErr == nil {
		kept = ac.Selected
	}
	c := judgeshadow.Compare(shadowTarget(ac.Track), kept, ac.ProbedDuration.Seconds(), ac.Candidates)
	slog.InfoContext(ctx, "acquisition.judge_shadow",
		logging.KeyTrackID, ac.Track.ID,
		"pipeline_url", c.PipelineURL,
		"judge_url", c.JudgeURL,
		"judge_reason", c.Reason,
		"verdict", c.Verdict,
	)
}

func shadowTarget(t TrackRef) titlematch.Target {
	return titlematch.Target{Title: t.Title, Artist: t.Artist, Album: t.Album, Length: t.CatalogDuration, Explicit: t.CatalogExplicit}
}

func keepFirstTierOutage(fallbackErr, firstTierErr error) error {
	if fallbackErr == nil || acqdomain.IsSourceUnavailable(fallbackErr) || !acqdomain.IsSourceUnavailable(firstTierErr) {
		return fallbackErr
	}
	return fmt.Errorf("%w (first tier: %w)", fallbackErr, firstTierErr)
}

func searchAndDownloadAnyTier(ctx context.Context, run *pipelineRun, p Pipeline) (afterDownload, error) {
	ac := run.ac
	downloaded, err := searchAndDownload(ctx, run, p)
	if err == nil || ac.Pass != PassFallbackSkipped || ctx.Err() != nil {
		return downloaded, err
	}
	slog.InfoContext(ctx, "acquisition.first_tier_failed_trying_fallback",
		logging.KeyTrackID, ac.Track.ID, logging.KeyError, redact.LogError(err))
	ac.Pass = PassFallbackOnly
	ac.clearCandidates()
	run.completed = nil
	downloaded, fallbackErr := searchAndDownload(ctx, run, p)
	return downloaded, keepFirstTierOutage(fallbackErr, err)
}

func searchAndDownload(ctx context.Context, run *pipelineRun, p Pipeline) (afterDownload, error) {
	searched, err := runStage(ctx, run, p.search, PipelineStart{})
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
	ac.Job.Progress().Stage(ctx, string(s.Name()))
	if ac.Selected != nil {
		ac.Job.Progress().Source(ac.Selected.URL)
	}
	slog.InfoContext(ctx, "pipeline step starting", "step", s.Name(), logging.KeyTrackID, ac.Track.ID)
}

func execStage[In, Out any](ctx context.Context, run *pipelineRun, s finalStage[In, Out], prev In) (Out, error) {
	var none Out

	if ctxErr := ctx.Err(); ctxErr != nil {
		rollback(ctx, run.completed, run.ac)
		return none, fmt.Errorf("pipeline cancelled: %w", ctxErr)
	}

	announceStage(ctx, run, s)

	stageCtx, stageDone := startStage(ctx, run.ac, run.now, string(s.Name()))
	out, execErr := s.Execute(stageCtx, run.ac, prev)
	stageDone(execErr)
	if execErr != nil {
		slog.ErrorContext(ctx, "pipeline step failed",
			"step", s.Name(), logging.KeyTrackID, run.ac.Track.ID, logging.KeyError, redact.LogError(execErr))
		rollback(ctx, run.completed, run.ac)
		return none, &StepError{Step: s.Name(), Err: execErr}
	}

	return out, nil
}

const rollbackBudget = 30 * time.Second

func rollback(ctx context.Context, completed []undoable, ac *AcquisitionContext) {
	rbCtx, cancel := background.Detach(ctx, rollbackBudget)
	defer cancel()
	ac.rollbackHandedOver = ac.Job.HandedOverByScheduler(ctx)

	for i := len(completed) - 1; i >= 0; i-- {
		step := completed[i]
		slog.InfoContext(rbCtx, "rolling back step", "step", step.Name())
		if err := step.Rollback(rbCtx, ac); err != nil {
			slog.ErrorContext(rbCtx, "rollback failed", "step", step.Name(), logging.KeyError, redact.LogError(err))
		}
	}
}

type AcquisitionContext struct {
	Job               ports.AcquisitionJob
	Track             TrackRef
	Identity          ports.RecordingIdentity
	Candidates        []ports.AudioCandidate
	SearchUnavailable error
	Ranked            []ports.AudioCandidate
	Selected          *ports.AudioCandidate
	TempPath          string
	TempDir           string
	AudioRef          string
	ProbedDuration    sharedtracklength.TrackLength
	DurationVerified  bool
	IdentityVerified  bool
	Verdict           AudioVerdict
	Confidence        float64
	Evidence          Evidence
	BestEffort        bool
	SourceFormat      ports.SourceFormat

	Rejections []rejectedcandidates.Entry
	Attempted  []AttemptedCandidate

	Replace ReplaceState

	Pass SearchPass

	PriorRejections []ports.PriorRejection

	Relabel *ports.TrackLabel

	rollbackHandedOver bool
}

func (ac *AcquisitionContext) Snapshot() AcquisitionContext {
	copied := *ac
	copied.Track.CatalogSources = slices.Clone(ac.Track.CatalogSources)
	copied.Identity.Sources = slices.Clone(ac.Identity.Sources)
	copied.Identity.MBIDs = slices.Clone(ac.Identity.MBIDs)
	copied.Candidates = slices.Clone(ac.Candidates)
	copied.Ranked = slices.Clone(ac.Ranked)
	copied.Selected = cloned(ac.Selected)
	copied.Evidence.Qualifiers = slices.Clone(ac.Evidence.Qualifiers)
	copied.Evidence.DurationDeltaSeconds = cloned(ac.Evidence.DurationDeltaSeconds)
	copied.Rejections = slices.Clone(ac.Rejections)
	copied.Attempted = slices.Clone(ac.Attempted)
	copied.Replace.ExcludeKeys = slices.Clone(ac.Replace.ExcludeKeys)
	copied.PriorRejections = slices.Clone(ac.PriorRejections)
	copied.Relabel = cloned(ac.Relabel)
	return copied
}

func cloned[T any](p *T) *T {
	if p == nil {
		return nil
	}
	value := *p
	return &value
}

func (ac *AcquisitionContext) clearCandidates() {
	ac.Candidates, ac.Ranked, ac.Selected = nil, nil, nil
}

func (ac *AcquisitionContext) retargetTo(label ports.TrackLabel) {
	ac.clearCandidates()
	ac.Pass = PassAllTiers
	ac.Identity = ports.RecordingIdentity{}
	ac.Track.Title, ac.Track.Album, ac.Track.Year = label.Title, label.Album, label.Year
	ac.Track.ISRC, ac.Track.CatalogMBID, ac.Track.CatalogDuration = label.ISRC, label.MBID, label.DurationSeconds
	ac.Track.CatalogSources, ac.Track.AudioSourceURL = nil, ""
	ac.Relabel = &label
}

func (ac *AcquisitionContext) refetchesCurrentSource() bool {
	return ac.Replace.Gate.RefetchesCurrentSource()
}

func (ac *AcquisitionContext) preserveCurrentAudio(track *ports.TrackSnapshot) {
	ac.Replace.PreservedRef = deref(track.AudioRef)
	ac.Track.AudioSourceURL = ""
	if !ac.refetchesCurrentSource() {
		ac.Replace.ExcludeKeys = mergeSourceKeys(
			ac.Replace.ExcludeKeys,
			sourceKey(deref(track.AudioSourceURL)),
		)
	}
}

func (ac *AcquisitionContext) admitFetched(fetched audioquality.Grade) error {
	if err := ac.Replace.Gate.AdmitFetched(fetched, ac.SourceFormat, ac.ProbedDuration.Seconds()); err != nil {
		return fmt.Errorf("admit fetched: %w", err)
	}
	return nil
}

type AttemptedCandidate struct {
	URL    string
	Window int
}

type ReplaceState struct {
	ExcludeKeys    []string
	PreservedRef   string
	StoredAcoustID string
	Gate           replacement.ReplacementGate

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
	return tracklength.SameRecording(recorded.ExpectedSeconds, current)
}

func (ac *AcquisitionContext) Provenance() downloadprovenance.AcquisitionProvenance {
	switch {
	case ac.BestEffort:
		return downloadprovenance.ProvenanceBestEffort
	case ac.IdentityVerified:
		return downloadprovenance.ProvenanceVerified
	case ac.DurationVerified && ac.lengths().Corroborated():
		return downloadprovenance.ProvenanceCorroborated
	default:
		return downloadprovenance.ProvenanceBestEffort
	}
}

func (ac *AcquisitionContext) StoredLength() sharedtracklength.TrackLength {
	if ac.ProbedDuration.Known() {
		return ac.ProbedDuration
	}
	if ac.Selected == nil {
		return sharedtracklength.TrackLength{}
	}
	reported, err := sharedtracklength.TrackLengthOf(ac.Selected.Duration)
	if err != nil {
		return sharedtracklength.TrackLength{}
	}
	return reported
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

func (s *pipeline) buildSteps(userId ids.UserId, trackId ids.TrackId) Pipeline {
	return buildCoreSteps(s.sources, s.audioTagger, s.audioStore, s.audioProber, s.identifier, s.recordingLookup, s.verifySkips, s.orphans, s.ledger, s.sleep, s.unit, s.now, s.workspace, s.downloadLimiter,
		[]func(*DownloadStep){
			WithConfidenceFloor(s.confidenceFloor),
			WithJudgeCuts(s.cuts),
		},
		WithStoreOwner(userId, trackId),
		WithStoreKeyPrefix(s.storeKeyPrefix)).
		withUpdateTrack(NewUpdateTrackStep(s.ledger, userId, trackId))
}

func CoreSteps(
	sources *SourceRegistry,
	tagger ports.AudioTagger,
	store ports.AudioWriter,
	prober ports.AudioProber,
	identifier ports.AudioIdentifier,
	lookup ports.RecordingLookup,
	skips ports.VerifySkipRecorder,
	orphans ports.OrphanedAudioRecorder,
	refs ports.AudioRefLookup,
	sleep clock.Sleeper,
	unit random.Unit,
	now clock.Func,
	workspace ports.ScratchFolder,
	limit *ConcurrencyLimit,
	downloadOpts []func(*DownloadStep),
	storeOpts ...func(*StoreStep),
) (Pipeline, error) {
	download, downloadErr := NewDownloadStep(sources, workspace, identifier, lookup, prober, skips, limit, now, append([]func(*DownloadStep){WithVerifyWidth(ProdVerifyWidth)}, downloadOpts...)...)
	tag, tagErr := NewTagStep(tagger)
	stored, storeErr := NewStoreStep(store, workspace, prober, orphans, refs, sleep, unit, storeOpts...)
	if err := errors.Join(downloadErr, tagErr, storeErr); err != nil {
		return Pipeline{}, err
	}
	return Pipeline{search: NewSearchStep(sources), selectBest: NewSelectStep(), download: download, tag: tag, store: stored, finish: endsAtStore, now: now}, nil
}

func buildCoreSteps(
	sources *SourceRegistry,
	tagger ports.AudioTagger,
	store ports.AudioWriter,
	prober ports.AudioProber,
	identifier ports.AudioIdentifier,
	lookup ports.RecordingLookup,
	skips ports.VerifySkipRecorder,
	orphans ports.OrphanedAudioRecorder,
	refs ports.AudioRefLookup,
	sleep clock.Sleeper,
	unit random.Unit,
	now clock.Func,
	workspace ports.ScratchFolder,
	limit *ConcurrencyLimit,
	downloadOpts []func(*DownloadStep),
	storeOpts ...func(*StoreStep),
) Pipeline {
	return Pipeline{
		search:     NewSearchStep(sources),
		selectBest: NewSelectStep(),
		download:   buildDownloadStep(sources, workspace, identifier, lookup, prober, skips, limit, now, append([]func(*DownloadStep){WithVerifyWidth(ProdVerifyWidth)}, downloadOpts...)...),
		tag:        &TagStep{tagger: tagger},
		store:      buildStoreStep(store, workspace, prober, orphans, refs, sleep, unit, storeOpts...),
		finish:     endsAtStore,
		now:        now,
	}
}
