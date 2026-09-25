package service

import (
	"altune/go-api/internal/acquisition/ports"
	"context"
	"fmt"
	"log/slog"
	"os"
)

// maxDownloadAttempts bounds the fetches one job may pay for, not the ranked
// positions it may walk: a candidate skipped before Fetch costs nothing, so it
// must not consume budget the job needs for a candidate worth downloading.
const maxDownloadAttempts = ports.EnoughCandidates

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

func (s *DownloadStep) Name() string { return stepNameDownload }

func (s *DownloadStep) Execute(ctx context.Context, ac *AcquisitionContext, _ afterSelect) (afterDownload, error) {
	var lastErr, unavailableErr error
	attempts := 0

	for i := range ac.Ranked {
		// A cancelled or timed-out job must surface as a cancellation, not keep
		// grinding through the remaining candidates and then report a permanent
		// download failure. Guard before each attempt so a mid-loop cancellation
		// stops here; withCancellation below covers a cancel that lands on the
		// last candidate's own Fetch without wrapping ctx.Err().
		if ctxErr := ctx.Err(); ctxErr != nil {
			return afterDownload{}, fmt.Errorf("download cancelled: %w", ctxErr)
		}
		if attempts >= maxDownloadAttempts {
			recordNotAttempted(ac, ac.Ranked[i:])
			break
		}
		if !ac.candidateDurationPlausible(ac.Ranked[i]) {
			recordImplausibleDuration(ctx, ac, ac.Ranked[i])
			continue
		}

		tmpDir, err := os.MkdirTemp("", tempDirPrefix+"*")
		if err != nil {
			return afterDownload{}, fmt.Errorf("create temp dir: %w", err)
		}

		attempts++
		selected, err := s.tryCandidate(ctx, ac, ac.Ranked[i], tmpDir)
		if selected {
			return afterDownload{}, nil
		}
		if err != nil {
			lastErr = err
			if ports.IsSourceUnavailable(err) {
				unavailableErr = err
			}
		}
	}

	if lastErr != nil {
		if unavailableErr != nil && !ports.IsSourceUnavailable(lastErr) {
			lastErr = fmt.Errorf("%w (last failure: %w)", unavailableErr, lastErr)
		}
		return afterDownload{}, withCancellation(ctx, fmt.Errorf("no candidate produced acceptable audio: %w", lastErr))
	}
	return afterDownload{}, fmt.Errorf("no candidate produced acceptable audio")
}

// recordNotAttempted gives every ranked candidate left untried by the attempt
// cap its own rejection, so the persisted summary counts the whole ranked list
// and shows the failure was capped rather than exhaustive.
func recordNotAttempted(ac *AcquisitionContext, untried []ports.AudioCandidate) {
	for _, c := range untried {
		ac.recordRejection(c.URL, c.Title, c.Source, RejectionNotAttempted,
			fmt.Sprintf("skipped after %d download attempts", maxDownloadAttempts))
	}
}

// recordImplausibleDuration rejects a candidate on the duration search already
// reported for it, so a three-hour mix is never downloaded and transcoded only
// to lose to the probe afterwards. It is the same stage the probe would record,
// with a reason naming search metadata as the source of the number.
func recordImplausibleDuration(ctx context.Context, ac *AcquisitionContext, candidate ports.AudioCandidate) {
	ac.recordRejection(candidate.URL, candidate.Title, candidate.Source, RejectionDuration,
		fmt.Sprintf("search duration %.0fs vs expected %.0fs", candidate.Duration, ac.Track.Duration))
	slog.InfoContext(ctx, "acquisition.candidate_skipped_duration",
		"track_id", ac.Track.ID,
		"url", candidate.URL,
		"source", candidate.Source,
		"search_duration", candidate.Duration,
		"expected_duration", ac.Track.Duration,
	)
}

// tryCandidate downloads and verifies one candidate into tmpDir. The temp dir
// is removed on every exit path — failure, rejection, or a panic in fetch or
// verify — except when the candidate is accepted, where it holds the audio file
// at ac.TempPath. Deferring the cleanup is what keeps a panicking step from
// leaking an altune-acquire-* dir: ac.TempPath is unset here, so neither the
// pipeline's rollback nor acquire.go's CleanupTemp could otherwise find it.
func (s *DownloadStep) tryCandidate(
	ctx context.Context,
	ac *AcquisitionContext,
	candidate ports.AudioCandidate,
	tmpDir string,
) (selected bool, err error) {
	defer func() {
		if !selected {
			os.RemoveAll(tmpDir)
		}
	}()

	filePath, err := s.fetcher.Fetch(ctx, candidate, tmpDir)
	if err != nil {
		ac.recordRejection(candidate.URL, candidate.Title, candidate.Source, RejectionDownload, "download failed")
		slog.WarnContext(ctx, "acquisition.candidate_download_failed",
			"track_id", ac.Track.ID, "url", candidate.URL, "source", candidate.Source,
			"error", logSafeError(err))
		return false, err
	}

	verified, rejection := s.verify(ctx, ac, candidate, filePath)
	if rejection != nil {
		ac.recordRejection(candidate.URL, candidate.Title, candidate.Source, rejection.stage, rejection.reason)
		return false, rejection.err
	}

	sel := candidate
	ac.Selected = &sel
	ac.TempPath = filePath
	ac.DurationVerified = verified.duration
	ac.IdentityVerified = verified.identity
	ac.ProbedDuration = verified.probed
	selected = true
	return true, nil
}

type verificationResult struct {
	duration bool
	identity bool
	probed   float64
}

// downloadRejection is why a downloaded candidate was discarded: reason is a
// safe, persistable summary (durations, stage), while err carries the full
// internal detail for the pipeline's last-error wrapping only.
type downloadRejection struct {
	stage  RejectionStage
	reason string
	err    error
}

func (s *DownloadStep) verify(
	ctx context.Context,
	ac *AcquisitionContext,
	candidate ports.AudioCandidate,
	filePath string,
) (verificationResult, *downloadRejection) {
	var result verificationResult

	if s.prober != nil && ac.Track.Duration > 0 {
		actual, err := s.prober.ProbeDuration(ctx, filePath)
		switch {
		case err != nil:
			slog.WarnContext(ctx, "acquisition.probe_failed_accepting",
				"track_id", ac.Track.ID, "url", candidate.URL, "source", candidate.Source,
				"error", logSafeError(err))
		case !ac.durationAcceptable(actual):
			slog.InfoContext(ctx, "acquisition.candidate_rejected_duration",
				"track_id", ac.Track.ID,
				"url", candidate.URL,
				"source", candidate.Source,
				"actual_duration", actual,
				"expected_duration", ac.Track.Duration,
				"authoritative", ac.Identity.Duration > 0,
			)
			return result, &downloadRejection{
				stage:  RejectionDuration,
				reason: fmt.Sprintf("duration %.0fs vs expected %.0fs", actual, ac.Track.Duration),
				err: fmt.Errorf("candidate %q duration %.0fs != expected %.0fs",
					candidate.URL, actual, ac.Track.Duration),
			}
		default:
			result.duration = true
			result.probed = actual
		}
	}

	if s.prober != nil {
		if err := s.prober.ValidateDecodable(ctx, filePath); err != nil {
			slog.WarnContext(ctx, "acquisition.candidate_rejected_undecodable",
				"track_id", ac.Track.ID, "url", candidate.URL, "source", candidate.Source,
				"error", logSafeError(err))
			return result, &downloadRejection{
				stage:  RejectionUndecodable,
				reason: "audio failed to decode",
				err:    fmt.Errorf("candidate %q undecodable: %w", candidate.URL, err),
			}
		}
	}

	if rejected := s.identify(ctx, ac, candidate, filePath, &result); rejected {
		return result, &downloadRejection{
			stage:  RejectionFingerprint,
			reason: "different recording",
			err:    fmt.Errorf("candidate %q is a different recording", candidate.URL),
		}
	}

	return result, nil
}

func (s *DownloadStep) identify(
	ctx context.Context,
	ac *AcquisitionContext,
	candidate ports.AudioCandidate,
	filePath string,
	result *verificationResult,
) (rejected bool) {
	if s.identifier == nil || ac.Identity.MBID == "" {
		return false
	}

	match, err := s.identifier.Identify(ctx, filePath)
	switch {
	case err != nil:
		slog.WarnContext(ctx, "acquisition.identify_failed",
			"track_id", ac.Track.ID, "url", candidate.URL, "source", candidate.Source,
			"error", logSafeError(err))
		return false
	case !match.Known():
		slog.InfoContext(ctx, "acquisition.identify_unknown",
			"track_id", ac.Track.ID, "url", candidate.URL, "source", candidate.Source)
		return false
	case match.Matches(ac.Identity.MBID), match.InCluster(ac.Identity.AcoustIDs):
		result.identity = true
		return false
	case len(ac.Identity.AcoustIDs) == 0:
		slog.InfoContext(ctx, "acquisition.identify_uncorroborated",
			"track_id", ac.Track.ID, "url", candidate.URL, "source", candidate.Source,
			"want_mbid", ac.Identity.MBID, "got_mbids", match.MBIDs)
		return false
	default:
		slog.InfoContext(ctx, "acquisition.candidate_rejected_fingerprint",
			"track_id", ac.Track.ID,
			"url", candidate.URL,
			"source", candidate.Source,
			"want_mbid", ac.Identity.MBID,
			"want_acoustids", ac.Identity.AcoustIDs,
			"got_acoustid", match.AcoustID,
			"got_mbids", match.MBIDs,
		)
		return true
	}
}

func (s *DownloadStep) Rollback(_ context.Context, ac *AcquisitionContext) error {
	if ac.TempPath != "" {
		os.RemoveAll(ac.TempPath)
	}
	return nil
}
