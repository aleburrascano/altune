package service

import (
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/parallel"
	"context"
	"fmt"
	"sync"
)

func (s *DownloadStep) executeWindowed(ctx context.Context, ac *AcquisitionContext) (afterDownload, error) {
	failures := newDownloadFailures(ac.SearchUnavailable)
	holds := holdBook{ws: s.workspace}
	defer holds.discard(ctx)
	pending := ac.Ranked
	used, windowIndex := 0, nextWindowIndex(ac.Attempted)

	for len(pending) > 0 {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return afterDownload{}, fmt.Errorf("download cancelled: %w", ctxErr)
		}
		if holds.muxedRetrySpent() {
			recordNotAttempted(ac, pending)
			break
		}
		var window []ports.AudioCandidate
		window, pending = s.nextWindow(ctx, ac, pending, used, &failures)
		if len(window) == 0 {
			break
		}
		used += len(window)
		recordWindow(ac, window, windowIndex)
		windowIndex++
		results := s.runWindow(ctx, ac, window)
		if downloaded, won := mergeWindow(ctx, s.workspace, ac, results, &holds, &failures); won {
			return downloaded, nil
		}
	}
	return s.settle(ctx, ac, &holds, &failures)
}

func (s *DownloadStep) nextWindow(
	ctx context.Context,
	ac *AcquisitionContext,
	pending []ports.AudioCandidate,
	used int,
	failures *downloadFailures,
) (window, rest []ports.AudioCandidate) {
	limit := min(s.width, MaxDownloadAttempts-used)
	for i, candidate := range pending {
		if len(window) == limit {
			return window, remainingAfter(ac, pending[i:], used+len(window))
		}
		if failures.sourceDead(candidate) {
			recordSourceDead(ac, candidate)
			continue
		}
		if !ac.lengths().Plausible(candidate) {
			recordImplausibleDuration(ctx, ac, candidate)
			continue
		}
		window = append(window, candidate)
	}
	return window, nil
}

func nextWindowIndex(attempted []AttemptedCandidate) int {
	next := 0
	for _, a := range attempted {
		next = max(next, a.Window+1)
	}
	return next
}

func recordWindow(ac *AcquisitionContext, window []ports.AudioCandidate, index int) {
	for _, candidate := range window {
		ac.Attempted = append(ac.Attempted, AttemptedCandidate{URL: candidate.URL, Window: index})
	}
}

func remainingAfter(ac *AcquisitionContext, rest []ports.AudioCandidate, started int) []ports.AudioCandidate {
	if started < MaxDownloadAttempts {
		return rest
	}
	recordNotAttempted(ac, rest)
	return nil
}

func (s *DownloadStep) runWindow(ctx context.Context, ac *AcquisitionContext, window []ports.AudioCandidate) []attempt {
	results := make([]attempt, len(window))
	cutoff := newSlotCutoff(len(window))
	g := parallel.New("acquisition.download_attempt_panic", len(window))
	for rank := range window {
		g.Go(ctx, func() {
			slotCtx, cancel := context.WithCancel(ctx)
			defer cancel()
			cutoff.enter(rank, cancel)
			results[rank] = s.guardedWindowAttempt(slotCtx, ac, window[rank])
			if results[rank].status == attemptAccepted && !results[rank].muxed() {
				cutoff.cancelAfter(rank)
			}
		})
	}
	_ = g.Wait()
	return results
}

type slotCutoff struct {
	mu      sync.Mutex
	cancels []context.CancelFunc
	from    int
}

func newSlotCutoff(slots int) *slotCutoff {
	return &slotCutoff{cancels: make([]context.CancelFunc, slots), from: slots}
}

func (c *slotCutoff) enter(rank int, cancel context.CancelFunc) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cancels[rank] = cancel
	if rank >= c.from {
		cancel()
	}
}

func (c *slotCutoff) cancelAfter(rank int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.from = min(c.from, rank+1)
	for _, cancel := range c.cancels[c.from:] {
		if cancel != nil {
			cancel()
		}
	}
}

func (s *DownloadStep) guardedWindowAttempt(ctx context.Context, ac *AcquisitionContext, candidate ports.AudioCandidate) attempt {
	var result attempt
	panicked := background.SafeRun(ctx, "acquisition.download_attempt_panic", func() { result = s.runWindowAttempt(ctx, ac, candidate) }, logging.KeyTrackID, ac.Track.ID)
	if panicked != nil {
		return attempt{candidate: candidate, rejection: &downloadRejection{
			stage: RejectDownload, reason: "download failed", err: panicked,
		}}
	}
	return result
}

func (s *DownloadStep) runWindowAttempt(ctx context.Context, ac *AcquisitionContext, candidate ports.AudioCandidate) attempt {
	tmpDir, err := s.workspace.Create("*")
	if err != nil {
		return attempt{candidate: candidate, rejection: &downloadRejection{
			stage: RejectDownload, reason: "download failed", err: fmt.Errorf("create temp dir: %w", err),
		}}
	}
	return s.runAttempt(ctx, ac, candidate, tmpDir)
}

func mergeWindow(ctx context.Context, ws ports.ScratchFolder, ac *AcquisitionContext, results []attempt, holds *holdBook, failures *downloadFailures) (afterDownload, bool) {
	var downloaded afterDownload
	won := false
	for _, result := range results {
		if won && result.status != attemptRejected {
			removeScratch(ctx, ws, result.tmpDir)
			continue
		}
		if result.status == attemptAccepted {
			downloaded, won = holds.resolveAccepted(ctx, ac, result)
			continue
		}
		result.applyTo(ctx, ac)
		failures.noteAttempt(result)
		holds.offer(ctx, result)
	}
	return downloaded, won
}
