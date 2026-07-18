package service

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"altune/go-api/internal/acquisition/ports"
)

// maxVerifyAttempts bounds how many ranked candidates DownloadStep will download
// while looking for one whose audio passes duration verification. Each attempt is
// a full download, so the cap keeps a pathological query from downloading the
// whole candidate list; in the common case the first candidate verifies.
const maxVerifyAttempts = 4

type DownloadStep struct {
	searcher ports.AudioSearcher
	prober   ports.AudioProber
}

func NewDownloadStep(searcher ports.AudioSearcher, opts ...func(*DownloadStep)) *DownloadStep {
	s := &DownloadStep{searcher: searcher}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// WithDownloadProber wires a duration prober so each downloaded file is verified
// against the track's expected duration before it is accepted. Without it (e.g.
// in unit tests, or when no expected duration exists) the first candidate is
// downloaded and accepted unverified — the prior behaviour.
func WithDownloadProber(p ports.AudioProber) func(*DownloadStep) {
	return func(s *DownloadStep) { s.prober = p }
}

func (s *DownloadStep) Name() string { return "download" }

// Execute downloads candidates best-first and accepts the first whose audio
// matches the track's expected duration. A candidate that downloads but verifies
// wrong (a 14:00 mix, a 0:30 preview) is discarded and the next is tried. When
// verification can't run — no prober, or the track has no expected duration —
// the first successful download is accepted, preserving prior behaviour.
func (s *DownloadStep) Execute(ctx context.Context, ac *AcquisitionContext) error {
	candidates := ac.Ranked

	verify := s.prober != nil && ac.Track.Duration > 0

	var lastErr error
	attempts := 0
	for i := range candidates {
		if attempts >= maxVerifyAttempts {
			break
		}
		attempts++
		c := candidates[i]

		tmpDir, err := os.MkdirTemp("", "altune-acquire-*")
		if err != nil {
			return fmt.Errorf("create temp dir: %w", err)
		}

		filePath, err := s.searcher.Download(ctx, c.URL, tmpDir)
		if err != nil {
			os.RemoveAll(tmpDir)
			lastErr = err
			slog.WarnContext(ctx, "acquisition.candidate_download_failed",
				"url", c.URL, "error", err)
			continue
		}

		if verify {
			actual, perr := s.prober.ProbeDuration(ctx, filePath)
			switch {
			case perr != nil:
				// Can't verify — accept rather than block acquisition on a prober
				// hiccup, but record it.
				slog.WarnContext(ctx, "acquisition.probe_failed_accepting",
					"url", c.URL, "error", perr)
			case !durationWithinTolerance(ac.Track.Duration, actual):
				slog.InfoContext(ctx, "acquisition.candidate_rejected_duration",
					"url", c.URL,
					"actual_duration", actual,
					"expected_duration", ac.Track.Duration,
				)
				os.RemoveAll(tmpDir)
				lastErr = fmt.Errorf("candidate %q duration %.0fs != expected %.0fs", c.URL, actual, ac.Track.Duration)
				continue
			}
		}

		// Decode gate: a candidate can have the right duration and a valid container
		// yet corrupt samples (the shipped-m4a defect). Reject it and try the next
		// rather than store audio that no player can decode. Runs whenever a prober
		// is wired, independent of expected duration.
		if s.prober != nil {
			if derr := s.prober.ValidateDecodable(ctx, filePath); derr != nil {
				slog.WarnContext(ctx, "acquisition.candidate_rejected_undecodable",
					"url", c.URL, "error", derr)
				os.RemoveAll(tmpDir)
				lastErr = fmt.Errorf("candidate %q undecodable: %w", c.URL, derr)
				continue
			}
		}

		sel := c
		ac.Selected = &sel
		ac.TempPath = filePath
		return nil
	}

	if lastErr != nil {
		return fmt.Errorf("no candidate produced acceptable audio: %w", lastErr)
	}
	return fmt.Errorf("no candidate produced acceptable audio")
}

func (s *DownloadStep) Rollback(_ context.Context, ac *AcquisitionContext) error {
	if ac.TempPath != "" {
		os.RemoveAll(ac.TempPath)
	}
	return nil
}
