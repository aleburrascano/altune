package service

import (
	"altune/go-api/internal/acquisition/ports"
	"context"
	"fmt"
	"os"
	"sync"
)

func (s *DownloadStep) executeWindowed(ctx context.Context, ac *AcquisitionContext) (afterDownload, error) {
	failures := downloadFailures{unavailable: ac.SearchUnavailable}
	pending := ac.Ranked
	used := 0

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
		results := s.runWindow(ctx, ac, window)
		if mergeWindow(ac, results, &failures) {
			return afterDownload{}, nil
		}
	}
	return afterDownload{}, failures.result(ctx)
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
			results[rank] = s.runWindowAttempt(ctxs[rank], ac, window[rank])
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

func (s *DownloadStep) runWindowAttempt(ctx context.Context, ac *AcquisitionContext, candidate ports.AudioCandidate) attempt {
	tmpDir, err := os.MkdirTemp("", tempDirPrefix+"*")
	if err != nil {
		return attempt{candidate: candidate, rejection: &downloadRejection{
			stage: RejectionDownload, reason: "download failed", err: fmt.Errorf("create temp dir: %w", err),
		}}
	}
	return s.runAttempt(ctx, ac, candidate, tmpDir)
}

func mergeWindow(ac *AcquisitionContext, results []attempt, failures *downloadFailures) bool {
	won := false
	for _, result := range results {
		if result.accepted && won {
			_ = os.RemoveAll(result.tmpDir)
			continue
		}
		result.applyTo(ac)
		failures.note(result.err())
		won = won || result.accepted
	}
	return won
}
