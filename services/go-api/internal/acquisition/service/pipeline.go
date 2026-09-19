package service

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/catalog/domain"
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"time"
)

// The pipeline's stage order is carried by the types, not by a slice literal.
// Each stage's Execute takes the token only the previous stage returns and
// returns the token the next stage needs, so RunPipeline can only compose the
// stages search→select→download→tag→store→update_track; any reorder is a
// compile error. Tokens are empty proofs: the work product still lives on the
// shared *AcquisitionContext.
type (
	pipelineStart struct{}
	afterSearch   struct{}
	afterSelect   struct{}
	afterDownload struct{}
	afterTag      struct{}
	afterStore    struct{}
	afterUpdate   struct{}
)

// The step names are a contract, not labels. reasonForStep turns each into the
// failure code persisted on the track, and the console and client match the
// values byte for byte, so renaming one here changes what a user is told.
const (
	stepNameSearch      = "search"
	stepNameSelect      = "select"
	stepNameDownload    = "download"
	stepNameTag         = "tag"
	stepNameStore       = "store"
	stepNameUpdateTrack = "update_track"
)

// undoable is the order-free half of a stage: its contract name and its
// rollback, which RunPipeline invokes in reverse completion order.
type undoable interface {
	Name() string
	Rollback(ctx context.Context, ac *AcquisitionContext) error
}

// stage is one pipeline step that may run only after the stage producing In.
type stage[In, Out any] interface {
	undoable
	Execute(ctx context.Context, ac *AcquisitionContext, prev In) (Out, error)
}

// Pipeline is the fixed-arity acquisition pipeline. Build it with CoreSteps
// (search through store) and, for the production service, withUpdateTrack.
type Pipeline struct {
	search      stage[pipelineStart, afterSearch]
	selectBest  stage[afterSearch, afterSelect]
	download    stage[afterSelect, afterDownload]
	tag         stage[afterDownload, afterTag]
	store       stage[afterTag, afterStore]
	updateTrack stage[afterStore, afterUpdate] // nil: the pipeline stops after store
}

func (p Pipeline) withUpdateTrack(s stage[afterStore, afterUpdate]) Pipeline {
	p.updateTrack = s
	return p
}

type StepError struct {
	Step string
	Err  error
}

func (e *StepError) Error() string { return fmt.Sprintf("step %s: %v", e.Step, e.Err) }
func (e *StepError) Unwrap() error { return e.Err }

// pipelineRun is one RunPipeline invocation's bookkeeping: the stages that
// completed (for rollback) and the stage currently executing (for panics).
type pipelineRun struct {
	ac        *AcquisitionContext
	reporter  jobReporter
	completed []undoable
	current   undoable
}

func RunPipeline(ctx context.Context, p Pipeline, ac *AcquisitionContext) (err error) {
	run := &pipelineRun{ac: ac, reporter: jobReporterFrom(ctx)}

	// A panic in any step must not skip rollback or propagate past this use
	// case: scheduler.go's recover is a last resort that bypasses acquire.go's
	// "mark track Failed" path, stranding the track at Pending forever. Mirror
	// registry.go's per-goroutine recover here — convert the panic into a
	// *StepError and roll back the completed steps so the normal failure path
	// runs.
	defer func() {
		if rec := recover(); rec != nil {
			step := "pipeline"
			if run.current != nil {
				step = run.current.Name()
			}
			slog.ErrorContext(ctx, "pipeline step panicked",
				"step", step, "track_id", ac.Track.ID, "panic", logSafeText(fmt.Sprint(rec)), "stack", string(debug.Stack()))
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

// runStage executes one stage under the pipeline's per-step contract: honor
// cancellation before starting, report the stage, and on failure roll back
// every completed stage and wrap the error in a *StepError.
func runStage[In, Out any](ctx context.Context, run *pipelineRun, s stage[In, Out], prev In) (Out, error) {
	var none Out
	ac := run.ac

	if ctxErr := ctx.Err(); ctxErr != nil {
		rollback(ctx, run.completed, ac)
		return none, fmt.Errorf("pipeline cancelled: %w", ctxErr)
	}

	run.current = s
	run.reporter.stage(s.Name())
	if ac.Selected != nil {
		run.reporter.source(ac.Selected.URL)
	}
	slog.InfoContext(ctx, "pipeline step starting", "step", s.Name(), "track_id", ac.Track.ID)

	out, execErr := s.Execute(ctx, ac, prev)
	if execErr != nil {
		slog.ErrorContext(ctx, "pipeline step failed",
			"step", s.Name(), "track_id", ac.Track.ID, "error", logSafeError(execErr))
		rollback(ctx, run.completed, ac)
		return none, &StepError{Step: s.Name(), Err: execErr}
	}

	run.completed = append(run.completed, s)
	return out, nil
}

// rollbackBudget is how long the compensations get once the job's own budget
// is gone.
const rollbackBudget = 30 * time.Second

// rollback compensates the completed stages in reverse. It detaches from ctx's
// cancellation because it runs precisely when ctx is already done — the
// acquireTimeout fired or the scheduler is shutting down — and a rollback on a
// dead context deletes nothing, orphaning the stored audio and stranding the
// track. ctx's values are kept so the compensations stay correlated to the job.
func rollback(ctx context.Context, completed []undoable, ac *AcquisitionContext) {
	rbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), rollbackBudget)
	defer cancel()

	for i := len(completed) - 1; i >= 0; i-- {
		step := completed[i]
		slog.InfoContext(rbCtx, "rolling back step", "step", step.Name())
		if err := step.Rollback(rbCtx, ac); err != nil {
			slog.ErrorContext(rbCtx, "rollback failed", "step", step.Name(), "error", logSafeError(err))
		}
	}
}

type AcquisitionContext struct {
	Track            TrackRef
	Identity         ports.RecordingIdentity
	Candidates       []ports.AudioCandidate
	Ranked           []ports.AudioCandidate
	Selected         *ports.AudioCandidate
	TempPath         string
	AudioRef         string
	ProbedDuration   float64
	DurationVerified bool
	IdentityVerified bool

	// Rejections accumulates, per candidate, why it was discarded across the
	// select and download steps so the failure is explainable beyond the logs.
	Rejections []CandidateRejection

	Replace ReplaceState
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

func (ac *AcquisitionContext) Provenance() domain.AcquisitionProvenance {
	switch {
	case ac.IdentityVerified:
		return domain.ProvenanceVerified
	case ac.DurationVerified && ac.lengthCorroborated():
		return domain.ProvenanceCorroborated
	default:
		return domain.ProvenanceBestEffort
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
