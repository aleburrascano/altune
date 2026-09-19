package service

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/catalog/domain"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type fileWritingSearcher struct {
	writeFile bool
	err       error
	gotURL    string
	gotDir    string
	calls     int
}

func (s *fileWritingSearcher) Search(_ context.Context, _ string) ([]ports.AudioCandidate, error) {
	return nil, nil
}

func (s *fileWritingSearcher) Name() string { return "filewriting" }

func (s *fileWritingSearcher) Find(_ context.Context, _ ports.FindRequest) ([]ports.AudioCandidate, error) {
	return nil, nil
}

func (s *fileWritingSearcher) Fetch(ctx context.Context, c ports.AudioCandidate, outDir string) (string, error) {
	return s.Download(ctx, c.URL, outDir)
}

func (s *fileWritingSearcher) Download(_ context.Context, url, outDir string) (string, error) {
	s.calls++
	s.gotURL = url
	s.gotDir = outDir
	if s.err != nil {
		return "", s.err
	}
	path := filepath.Join(outDir, "track.mp3")
	if s.writeFile {
		if err := os.WriteFile(path, []byte("audio-bytes"), 0o644); err != nil {
			return "", err
		}
	}
	return path, nil
}

func TestDownloadStep_Execute_Success(t *testing.T) {
	searcher := &fileWritingSearcher{writeFile: true}
	step := NewDownloadStep(searcher)
	ac := &AcquisitionContext{Ranked: []ports.AudioCandidate{{URL: "https://example.com/x"}}}

	if _, err := step.Execute(context.Background(), ac, afterSelect{}); err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(ac.TempPath))

	if ac.TempPath == "" {
		t.Fatal("expected TempPath to be set")
	}
	if _, err := os.Stat(ac.TempPath); err != nil {
		t.Errorf("downloaded file should exist: %v", err)
	}
	if searcher.gotURL != "https://example.com/x" {
		t.Errorf("download URL = %q, want the selected candidate URL", searcher.gotURL)
	}
}

func TestDownloadStep_Execute_NoSelected(t *testing.T) {
	step := NewDownloadStep(&fileWritingSearcher{})
	if _, err := step.Execute(context.Background(), &AcquisitionContext{}, afterSelect{}); err == nil {
		t.Fatal("expected error when no candidate is selected")
	}
}

func TestDownloadStep_Execute_DownloadError_CleansTempDir(t *testing.T) {
	searcher := &fileWritingSearcher{err: errors.New("boom")}
	step := NewDownloadStep(searcher)
	ac := &AcquisitionContext{Ranked: []ports.AudioCandidate{{URL: "https://example.com/x"}}}

	if _, err := step.Execute(context.Background(), ac, afterSelect{}); err == nil {
		t.Fatal("expected download error")
	}
	if ac.TempPath != "" {
		t.Errorf("TempPath must stay empty on failure, got %q", ac.TempPath)
	}
	if searcher.gotDir != "" {
		if _, err := os.Stat(searcher.gotDir); !os.IsNotExist(err) {
			t.Errorf("temp dir %q should be removed after a download error", searcher.gotDir)
		}
	}
}

func TestDownloadStep_Rollback_RemovesTempFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "track.mp3")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	step := NewDownloadStep(&fileWritingSearcher{})
	if err := step.Rollback(context.Background(), &AcquisitionContext{TempPath: file}); err != nil {
		t.Fatalf("Rollback error: %v", err)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Error("temp file should be removed after rollback")
	}
}

// Issue #963: a cancelled or timed-out job must surface as a cancellation, not
// keep iterating candidates and then report a permanent "download failed".

// cancellingFetcher ends the job context on its first Fetch, then fails the way
// a real downloader does: with an error that does not wrap ctx.Err(). calls
// records how many candidates were attempted.
type cancellingFetcher struct {
	cancel func()
	err    error
	calls  int
}

func (f *cancellingFetcher) Fetch(_ context.Context, _ ports.AudioCandidate, _ string) (string, error) {
	f.calls++
	if f.cancel != nil {
		f.cancel()
	}
	return "", f.err
}

func TestDownloadStep_CancelledMidLoop_ReportsCancellationAndStops(t *testing.T) {
	for _, tt := range []struct {
		name    string
		ctxErr  error
		newCtx  func() (context.Context, context.CancelFunc)
		fetch   error
		wantMax int // max candidates that should be attempted before bailing
	}{
		{
			name:  "cancel mid-loop, adapter drops ctx cause",
			fetch: errors.New("yt-dlp: exit 1"),
			newCtx: func() (context.Context, context.CancelFunc) {
				return context.WithCancel(context.Background())
			},
			wantMax: 1,
		},
		{
			name:  "cancel mid-loop, adapter wraps ctx cause",
			fetch: context.Canceled,
			newCtx: func() (context.Context, context.CancelFunc) {
				return context.WithCancel(context.Background())
			},
			wantMax: 1,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := tt.newCtx()
			defer cancel()
			fetcher := &cancellingFetcher{cancel: cancel, err: tt.fetch}
			step := NewDownloadStep(fetcher)
			ac := &AcquisitionContext{Ranked: []ports.AudioCandidate{
				{URL: "https://example.com/a"},
				{URL: "https://example.com/b"},
				{URL: "https://example.com/c"},
			}}

			_, err := step.Execute(ctx, ac, afterSelect{})
			if err == nil {
				t.Fatal("expected an error from a cancelled download")
			}
			if got := failureReason(&StepError{Step: "download", Err: err}); got != string(domain.FailureAcquisitionCancelled) {
				t.Errorf("failureReason = %q, want %q", got, domain.FailureAcquisitionCancelled)
			}
			if fetcher.calls > tt.wantMax {
				t.Errorf("attempted %d candidates after cancellation, want <= %d", fetcher.calls, tt.wantMax)
			}
		})
	}
}

// The single-candidate path exercises the withCancellation wrap: the cancel
// lands on the only candidate's Fetch, so the loop ends before its guard can
// re-check ctx. The exhausted-candidates return must still classify as cancel.
func TestDownloadStep_CancelledOnLastCandidate_ReportsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fetcher := &cancellingFetcher{cancel: cancel, err: errors.New("connection reset")}
	step := NewDownloadStep(fetcher)
	ac := &AcquisitionContext{Ranked: []ports.AudioCandidate{{URL: "https://example.com/only"}}}

	_, err := step.Execute(ctx, ac, afterSelect{})
	if err == nil {
		t.Fatal("expected an error from a cancelled download")
	}
	if got := failureReason(&StepError{Step: "download", Err: err}); got != string(domain.FailureAcquisitionCancelled) {
		t.Errorf("failureReason = %q, want %q", got, domain.FailureAcquisitionCancelled)
	}
}

// An already-expired deadline must bail before the first attempt runs.
func TestDownloadStep_DeadlineExpiredBeforeStart_ReportsCancellation(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	fetcher := &cancellingFetcher{err: errors.New("unused")}
	step := NewDownloadStep(fetcher)
	ac := &AcquisitionContext{Ranked: []ports.AudioCandidate{{URL: "https://example.com/a"}}}

	_, err := step.Execute(ctx, ac, afterSelect{})
	if err == nil {
		t.Fatal("expected an error from an expired deadline")
	}
	if got := failureReason(&StepError{Step: "download", Err: err}); got != string(domain.FailureAcquisitionCancelled) {
		t.Errorf("failureReason = %q, want %q", got, domain.FailureAcquisitionCancelled)
	}
	if fetcher.calls != 0 {
		t.Errorf("attempted %d candidates with an expired deadline, want 0", fetcher.calls)
	}
}

// Regression: a genuine download failure under a live context keeps its
// permanent reason and does exhaust the candidate list.
func TestDownloadStep_GenuineFailure_KeepsDownloadReasonAndTriesAll(t *testing.T) {
	fetcher := &cancellingFetcher{err: errors.New("yt-dlp: exit 1")}
	step := NewDownloadStep(fetcher)
	ac := &AcquisitionContext{Ranked: []ports.AudioCandidate{
		{URL: "https://example.com/a"},
		{URL: "https://example.com/b"},
	}}

	_, err := step.Execute(context.Background(), ac, afterSelect{})
	if err == nil {
		t.Fatal("expected a download error")
	}
	if got := failureReason(&StepError{Step: "download", Err: err}); got != string(domain.FailureDownloadFailed) {
		t.Errorf("failureReason = %q, want %q", got, domain.FailureDownloadFailed)
	}
	if fetcher.calls != 2 {
		t.Errorf("attempted %d candidates, want 2 (whole list under a live context)", fetcher.calls)
	}
}

// Issue #1976: search already reports a duration per candidate, so a candidate
// the duration gate is certain to reject must never be downloaded and
// transcoded first — the wasted minutes are what starve the right candidate.

func TestDownloadStep_ImplausibleSearchDuration_IsSkippedBeforeFetch(t *testing.T) {
	const mix = "https://example.com/three-hour-mix"
	searcher := &fileWritingSearcher{writeFile: true}
	step := NewDownloadStep(searcher)
	ac := &AcquisitionContext{
		Track: TrackRef{Title: "X", Artist: "Y", Duration: 200},
		Ranked: []ports.AudioCandidate{
			{URL: mix, Duration: 10800},
			{URL: "https://example.com/track", Duration: 201},
		},
	}

	if _, err := step.Execute(context.Background(), ac, afterSelect{}); err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(ac.TempPath))

	if searcher.calls != 1 {
		t.Errorf("fetched %d candidates, want 1: the mix must never be downloaded", searcher.calls)
	}
	if searcher.gotURL != "https://example.com/track" {
		t.Errorf("fetched %q, want the candidate whose search duration is plausible", searcher.gotURL)
	}
	if len(ac.Rejections) != 1 {
		t.Fatalf("rejections = %+v, want exactly one, for the mix", ac.Rejections)
	}
	if got := ac.Rejections[0]; got.URL != mix || got.Stage != RejectionDuration {
		t.Errorf("rejection = %+v, want %q at stage %q", got, mix, RejectionDuration)
	}
}

// A search duration only ever rules a candidate out. Every case where the number
// is absent or not the search's to judge must still reach the probe, which is
// the authoritative gate.
func TestDownloadStep_CandidateSearchDurationCannotDisqualify_IsStillFetched(t *testing.T) {
	for _, tt := range []struct {
		name      string
		track     TrackRef
		candidate ports.AudioCandidate
	}{
		{
			name:      "search reported no duration",
			track:     TrackRef{Duration: 200},
			candidate: ports.AudioCandidate{URL: "https://example.com/x"},
		},
		{
			name:      "a catalog resolved the candidate",
			track:     TrackRef{Duration: 200},
			candidate: ports.AudioCandidate{URL: "https://example.com/x", Duration: 10800, Resolved: true},
		},
		{
			name:      "the track has no saved duration to compare against",
			track:     TrackRef{},
			candidate: ports.AudioCandidate{URL: "https://example.com/x", Duration: 10800},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			searcher := &fileWritingSearcher{writeFile: true}
			step := NewDownloadStep(searcher)
			ac := &AcquisitionContext{Track: tt.track, Ranked: []ports.AudioCandidate{tt.candidate}}

			if _, err := step.Execute(context.Background(), ac, afterSelect{}); err != nil {
				t.Fatalf("Execute error: %v", err)
			}
			defer os.RemoveAll(filepath.Dir(ac.TempPath))

			if searcher.calls != 1 {
				t.Errorf("fetched %d candidates, want 1", searcher.calls)
			}
			if len(ac.Rejections) != 0 {
				t.Errorf("rejections = %+v, want none", ac.Rejections)
			}
		})
	}
}

// The attempt cap exists to bound what a job pays for. A candidate skipped
// before Fetch costs nothing, so it must not spend the budget the job still
// needs for a candidate worth downloading further down the ranking.
func TestDownloadStep_SkippedCandidatesDoNotSpendTheAttemptBudget(t *testing.T) {
	ranked := make([]ports.AudioCandidate, 0, maxDownloadAttempts+1)
	for i := 0; i < maxDownloadAttempts; i++ {
		ranked = append(ranked, ports.AudioCandidate{URL: "https://example.com/mix", Duration: 10800})
	}
	ranked = append(ranked, ports.AudioCandidate{URL: "https://example.com/track", Duration: 200})

	searcher := &fileWritingSearcher{writeFile: true}
	step := NewDownloadStep(searcher)
	ac := &AcquisitionContext{Track: TrackRef{Duration: 200}, Ranked: ranked}

	if _, err := step.Execute(context.Background(), ac, afterSelect{}); err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(ac.TempPath))

	if searcher.gotURL != "https://example.com/track" {
		t.Errorf("fetched %q, want the candidate past %d skipped ones", searcher.gotURL, maxDownloadAttempts)
	}
}

// panicProber explodes the moment verification touches the downloaded file,
// standing in for any probe/identify dependency that panics mid-pipeline.
type panicProber struct{}

func (panicProber) ProbeDuration(context.Context, string) (float64, error) {
	panic("prober exploded")
}

func (panicProber) ValidateDecodable(context.Context, string) error { return nil }

// TestDownloadStep_PanicDuringVerify_CleansTempDir reproduces the leak that
// survives #340: the per-candidate temp dir is created, the fetch succeeds, then
// verify panics. ac.TempPath is only set on the success path, so neither the
// pipeline's recover -> Rollback nor acquire.go's CleanupTemp can find this dir.
// Without deferred cleanup at the MkdirTemp site the altune-acquire-* dir leaks.
func TestDownloadStep_PanicDuringVerify_CleansTempDir(t *testing.T) {
	searcher := &fileWritingSearcher{writeFile: true}
	step := NewDownloadStep(searcher, WithDownloadProber(panicProber{}))
	ac := &AcquisitionContext{
		Track:  TrackRef{Title: "X", Artist: "Y", Duration: 226},
		Ranked: []ports.AudioCandidate{{URL: "https://example.com/x", Duration: 226}},
	}

	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("expected verify to panic")
			}
		}()
		_, _ = step.Execute(context.Background(), ac, afterSelect{})
	}()

	if searcher.gotDir == "" {
		t.Fatal("expected a temp dir to have been created before the panic")
	}
	if _, err := os.Stat(searcher.gotDir); !os.IsNotExist(err) {
		os.RemoveAll(searcher.gotDir)
		t.Errorf("temp dir %q leaked after a panic during verify", searcher.gotDir)
	}
	if ac.TempPath != "" {
		t.Errorf("TempPath must stay empty when verify panics, got %q", ac.TempPath)
	}
}
