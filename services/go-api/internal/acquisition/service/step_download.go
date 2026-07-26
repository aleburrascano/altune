package service

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"altune/go-api/internal/acquisition/ports"
)

const maxVerifyAttempts = 8

type candidateFetcher interface {
	Fetch(ctx context.Context, candidate ports.AudioCandidate, outDir string) (string, error)
}

type DownloadStep struct {
	fetcher    candidateFetcher
	prober     ports.AudioProber
	identifier ports.AudioIdentifier
}

func NewDownloadStep(fetcher candidateFetcher, opts ...func(*DownloadStep)) *DownloadStep {
	s := &DownloadStep{fetcher: fetcher}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func WithDownloadProber(p ports.AudioProber) func(*DownloadStep) {
	return func(s *DownloadStep) { s.prober = p }
}

func WithDownloadIdentifier(i ports.AudioIdentifier) func(*DownloadStep) {
	return func(s *DownloadStep) { s.identifier = i }
}

func (s *DownloadStep) Name() string { return "download" }

func (s *DownloadStep) Execute(ctx context.Context, ac *AcquisitionContext) error {
	var lastErr error
	attempts := 0

	for i := range ac.Ranked {
		if attempts >= maxVerifyAttempts {
			break
		}
		attempts++
		candidate := ac.Ranked[i]

		tmpDir, err := os.MkdirTemp("", "altune-acquire-*")
		if err != nil {
			return fmt.Errorf("create temp dir: %w", err)
		}

		filePath, err := s.fetcher.Fetch(ctx, candidate, tmpDir)
		if err != nil {
			os.RemoveAll(tmpDir)
			lastErr = err
			slog.WarnContext(ctx, "acquisition.candidate_download_failed",
				"url", candidate.URL, "source", candidate.Source, "error", err)
			continue
		}

		rejection, verified := s.verify(ctx, ac, candidate, filePath)
		if rejection != nil {
			os.RemoveAll(tmpDir)
			lastErr = rejection
			continue
		}

		selected := candidate
		ac.Selected = &selected
		ac.TempPath = filePath
		ac.DurationVerified = verified.duration
		ac.IdentityVerified = verified.identity
		return nil
	}

	if lastErr != nil {
		return fmt.Errorf("no candidate produced acceptable audio: %w", lastErr)
	}
	return fmt.Errorf("no candidate produced acceptable audio")
}

type verificationResult struct {
	duration bool
	identity bool
}

func (s *DownloadStep) verify(
	ctx context.Context,
	ac *AcquisitionContext,
	candidate ports.AudioCandidate,
	filePath string,
) (error, verificationResult) {
	var result verificationResult

	if s.prober != nil && ac.Track.Duration > 0 {
		actual, err := s.prober.ProbeDuration(ctx, filePath)
		switch {
		case err != nil:
			slog.WarnContext(ctx, "acquisition.probe_failed_accepting",
				"url", candidate.URL, "error", err)
		case !ac.durationAcceptable(actual):
			slog.InfoContext(ctx, "acquisition.candidate_rejected_duration",
				"url", candidate.URL,
				"actual_duration", actual,
				"expected_duration", ac.Track.Duration,
				"authoritative", ac.Identity.Duration > 0,
			)
			return fmt.Errorf("candidate %q duration %.0fs != expected %.0fs",
				candidate.URL, actual, ac.Track.Duration), result
		default:
			result.duration = true
		}
	}

	if s.prober != nil {
		if err := s.prober.ValidateDecodable(ctx, filePath); err != nil {
			slog.WarnContext(ctx, "acquisition.candidate_rejected_undecodable",
				"url", candidate.URL, "error", err)
			return fmt.Errorf("candidate %q undecodable: %w", candidate.URL, err), result
		}
	}

	s.identify(ctx, ac, candidate, filePath, &result)

	return nil, result
}

func (s *DownloadStep) identify(
	ctx context.Context,
	ac *AcquisitionContext,
	candidate ports.AudioCandidate,
	filePath string,
	result *verificationResult,
) {
	if s.identifier == nil || ac.Identity.MBID == "" {
		return
	}

	match, err := s.identifier.Identify(ctx, filePath)
	switch {
	case err != nil:
		slog.WarnContext(ctx, "acquisition.identify_failed",
			"url", candidate.URL, "error", err)
	case !match.Known():
		slog.InfoContext(ctx, "acquisition.identify_unknown",
			"url", candidate.URL)
	case match.Matches(ac.Identity.MBID):
		result.identity = true
	default:
		slog.InfoContext(ctx, "acquisition.identify_uncorroborated",
			"url", candidate.URL, "want_mbid", ac.Identity.MBID, "got_mbids", match.MBIDs)
	}
}

func (s *DownloadStep) Rollback(_ context.Context, ac *AcquisitionContext) error {
	if ac.TempPath != "" {
		os.RemoveAll(ac.TempPath)
	}
	return nil
}
