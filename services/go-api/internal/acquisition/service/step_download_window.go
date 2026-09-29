package service

import (
	"altune/go-api/internal/acquisition/ports"
	"context"
	"fmt"
	"log/slog"
	"os"
	"runtime/debug"
	"sync"
)

func (s *DownloadStep) executeWindowed(ctx context.Context, ac *AcquisitionContext) (afterDownload, error) {
	failures := downloadFailures{unavailable: ac.SearchUnavailable}
	var holds holdBook
	defer holds.discard()
	pending := ac.Ranked
	used, windowIndex := 0, 0

	for len(pending) > 0 {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return afterDownload{}, fmt.Errorf("download cancelled: %w", ctxErr)
		}
		var window []ports.AudioCandidate
		window, pending = s.nextWindow(ctx, ac, pending, used)
		if len(window) == 0 {
			break
		}
		used += len(window)
		recordWindow(ac, window, windowIndex)
		windowIndex++
		results := s.runWindow(ctx, ac, window)
		if mergeWindow(ctx, ac, results, &holds, &failures) {
			return afterDownload{}, nil
		}
	}
	return afterDownload{}, s.settle(ctx, ac, &holds, &failures)
}

func (s *DownloadStep) nextWindow(
	ctx context.Context,
	ac *AcquisitionContext,
	pending []ports.AudioCandidate,
	used int,
) (window, rest []ports.AudioCandidate) {
	limit := min(s.width, maxDownloadAttempts-used)
	for i, candidate := range pending {
		if len(window) == limit {
			return window, remainingAfter(ac, pending[i:], used+len(window))
		}
		if !ac.candidateDurationPlausible(candidate) {
			recordImplausibleDuration(ctx, ac, candidate)
			continue
		}
		window = append(window, candidate)
	}
	return window, nil
}

func recordWindow(ac *AcquisitionContext, window []ports.AudioCandidate, index int) {
	for _, candidate := range window {
		ac.Attempted = append(ac.Attempted, AttemptedCandidate{URL: candidate.URL, Window: index})
	}
}

func remainingAfter(ac *AcquisitionContext, rest []ports.AudioCandidate, started int) []ports.AudioCandidate {
	if started < maxDownloadAttempts {
		return rest
	}
	recordNotAttempted(ac, rest)
	return nil
}

func (s *DownloadStep) runWindow(ctx context.Context, ac *AcquisitionContext, window []ports.AudioCandidate) []attempt {
	ctxs, cancels := windowContexts(ctx, len(window))
	defer cancelAll(cancels)

	results := make([]attempt, len(window))
	var wg sync.WaitGroup
	var mu sync.Mutex
	for i := range window {
		wg.Add(1)
		go func(rank int) {
			defer wg.Done()
			results[rank] = s.guardedWindowAttempt(ctxs[rank], ac, window[rank])
			if results[rank].accepted {
				mu.Lock()
				cancelAll(cancels[rank+1:])
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	return results
}

func windowContexts(ctx context.Context, n int) ([]context.Context, []context.CancelFunc) {
	ctxs := make([]context.Context, n)
	cancels := make([]context.CancelFunc, n)
	for i := range n {
		ctxs[i], cancels[i] = context.WithCancel(ctx)
	}
	return ctxs, cancels
}

func cancelAll(cancels []context.CancelFunc) {
	for _, cancel := range cancels {
		cancel()
	}
}

func (s *DownloadStep) guardedWindowAttempt(ctx context.Context, ac *AcquisitionContext, candidate ports.AudioCandidate) (result attempt) {
	defer func() {
		if rec := recover(); rec != nil {
			slog.ErrorContext(ctx, "download attempt panicked",
				"track_id", ac.Track.ID, "panic", logSafeText(fmt.Sprint(rec)), "stack", string(debug.Stack()))
			result = attempt{candidate: candidate, rejection: &downloadRejection{
				stage: RejectionDownload, reason: "download failed", err: fmt.Errorf("attempt panicked: %v", rec),
			}}
		}
	}()
	return s.runWindowAttempt(ctx, ac, candidate)
}

func (s *DownloadStep) runWindowAttempt(ctx context.Context, ac *AcquisitionContext, candidate ports.AudioCandidate) attempt {
	tmpDir, err := os.MkdirTemp("", tempDirPrefix+"*")
	if err != nil {
		return attempt{candidate: candidate, rejection: &downloadRejection{
			stage: RejectionDownload, reason: "download failed", err: fmt.Errorf("create temp dir: %w", err),
		}}
	}
	return s.runAttempt(ctx, ac, candidate, tmpDir)
}

func mergeWindow(ctx context.Context, ac *AcquisitionContext, results []attempt, holds *holdBook, failures *downloadFailures) bool {
	won := false
	for _, result := range results {
		if won && (result.accepted || result.held) {
			_ = os.RemoveAll(result.tmpDir)
			continue
		}
		result.applyTo(ctx, ac)
		failures.note(result.err())
		holds.offer(result)
		won = won || result.accepted
	}
	return won
}
