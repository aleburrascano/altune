package service

import (
	"altune/go-api/internal/acquisition/ports"
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

type undoable interface {
	Name() StepName
	Rollback(ctx context.Context, ac *AcquisitionContext) error
}

type stage[In, Out any] interface {
	undoable
	Execute(ctx context.Context, ac *AcquisitionContext, prev In) (Out, error)
}

type Pipeline struct {
	search      stage[pipelineStart, afterSearch]
	selectBest  stage[afterSearch, afterSelect]
	download    stage[afterSelect, afterDownload]
	tag         stage[afterDownload, afterTag]
	store       stage[afterTag, afterStore]
	updateTrack stage[afterStore, afterUpdate]
}

func (p Pipeline) withUpdateTrack(s stage[afterStore, afterUpdate]) Pipeline {
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
	current   undoable
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

	searched, err := runStage(ctx, run, p.search, pipelineStart{})
	if err != nil {
		return err
	}
	selected, err := runStage(ctx, run, p.selectBest, searched)
	if err != nil {
		return err
	}
	downloaded, err := runStage(ctx, run, p.download, selected)
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
	_, err = runStage(ctx, run, p.updateTrack, stored)
	return err
}

func runStage[In, Out any](ctx context.Context, run *pipelineRun, s stage[In, Out], prev In) (Out, error) {
	var none Out
	ac := run.ac

	if ctxErr := ctx.Err(); ctxErr != nil {
		rollback(ctx, run.completed, ac)
		return none, fmt.Errorf("pipeline cancelled: %w", ctxErr)
	}

	run.current = s
	run.reporter.stage(string(s.Name()))
	if ac.Selected != nil {
		run.reporter.source(ac.Selected.URL)
	}
	slog.InfoContext(ctx, "pipeline step starting", "step", s.Name(), "track_id", ac.Track.ID)

	start := time.Now()
	out, execErr := s.Execute(ctx, ac, prev)
	timeStage(ctx, string(s.Name()), start)
	if execErr != nil {
		slog.ErrorContext(ctx, "pipeline step failed",
			"step", s.Name(), "track_id", ac.Track.ID, "error", redact.LogError(execErr))
		rollback(ctx, run.completed, ac)
		return none, &StepError{Step: s.Name(), Err: execErr}
	}

	run.completed = append(run.completed, s)
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

	PriorRejectedKeys []string
}

type AttemptedCandidate struct {
	URL    string
	Window int
}

type ReplaceState struct {
	ExcludeKeys   []string
	PreservedRef  string
	SkipTopRanked bool
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

func (ac *AcquisitionContext) priorRejected(url string) bool {
	key := sourceKey(url)
	for _, rejected := range ac.PriorRejectedKeys {
		if rejected == key {
			return true
		}
	}
	return false
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
	Duration    float64
	ISRC        string
	Year        int
	TrackNumber int
	AlbumArtist string
	Genre       string
}

func (s *AcquireTrackAudioService) buildSteps(userId shared.UserId, trackId shared.TrackId) Pipeline {
	return CoreSteps(s.sources, s.audioTagger, s.audioStore, s.audioProber, s.identifier, s.workspace,
		[]func(*DownloadStep){
			WithStepDownloadLimiter(s.downloadLimiter),
			WithConfidenceFloor(s.confidenceFloor),
			WithStepVerifySkips(s.verifySkips),
		},
		WithStoreAudioRefGuard(s.trackRepo, trackId),
		WithStoreOrphanQueue(s.orphans, userId),
		WithStoreKeyPrefix(s.storeKeyPrefix)).
		withUpdateTrack(NewUpdateTrackStep(s.trackRepo, userId, trackId))
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
		download:   NewDownloadStep(sources, append([]func(*DownloadStep){WithDownloadProber(prober), WithDownloadIdentifier(identifier), WithVerifyWidth(2), WithStepTempWorkspace(workspace)}, downloadOpts...)...),
		tag:        NewTagStep(tagger),
		store:      NewStoreStep(store, append([]func(*StoreStep){WithStoreProber(prober), WithStoreWorkspace(workspace)}, storeOpts...)...),
	}
}
