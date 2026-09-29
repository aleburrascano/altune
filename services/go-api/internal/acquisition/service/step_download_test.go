package service

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/catalog/domain"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
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
		wantMax int
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

type panicProber struct{}

func (panicProber) ProbeDuration(context.Context, string) (float64, error) {
	panic("prober exploded")
}

func (panicProber) ValidateDecodable(context.Context, string) error { return nil }

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

type sequenceFetcher struct {
	errs  []error
	calls int
}

func (f *sequenceFetcher) Fetch(_ context.Context, _ ports.AudioCandidate, _ string) (string, error) {
	err := f.errs[f.calls]
	f.calls++
	return "", err
}

func TestDownloadStep_EarlierUnavailableSurvivesADifferentLastFailure(t *testing.T) {
	fetcher := &sequenceFetcher{errs: []error{
		&ports.SourceUnavailableError{Source: "ytdlp", Err: errors.New("HTTP Error 429")},
		errors.New("exit 1"),
	}}
	ac := &AcquisitionContext{Ranked: []ports.AudioCandidate{{URL: "u1"}, {URL: "u2"}}}

	_, err := NewDownloadStep(fetcher).Execute(context.Background(), ac, afterSelect{})

	if code := failureCode(&StepError{Step: stepNameDownload, Err: err}); code != domain.FailureSourceUnavailable {
		t.Fatalf("failure code = %s, want %s", code, domain.FailureSourceUnavailable)
	}
}

type queueProber struct {
	durations  []float64
	calls      int
	decodeErrs []error
	decCalls   int
}

func (p *queueProber) ProbeDuration(_ context.Context, _ string) (float64, error) {
	d := p.durations[p.calls]
	p.calls++
	return d, nil
}

func (p *queueProber) ValidateDecodable(_ context.Context, _ string) error {
	var err error
	if p.decCalls < len(p.decodeErrs) {
		err = p.decodeErrs[p.decCalls]
	}
	p.decCalls++
	return err
}

func TestDurationWithinTolerance(t *testing.T) {
	tests := []struct {
		name             string
		expected, actual float64
		want             bool
	}{
		{name: "exact", expected: 226, actual: 226, want: true},
		{name: "few seconds off", expected: 226, actual: 231, want: true},
		{name: "within fraction", expected: 300, actual: 318, want: true},
		{name: "14-minute mix", expected: 226, actual: 840, want: false},
		{name: "30s preview", expected: 226, actual: 30, want: false},
		{name: "short track uses absolute slack", expected: 40, actual: 52, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := durationWithinTolerance(tt.expected, tt.actual); got != tt.want {
				t.Errorf("durationWithinTolerance(%v, %v) = %v, want %v", tt.expected, tt.actual, got, tt.want)
			}
		})
	}
}

func TestDownloadStep_VerifiesAndFallsBack(t *testing.T) {
	searcher := &fileWritingSearcher{writeFile: true}
	prober := &queueProber{durations: []float64{840, 227}}
	step := NewDownloadStep(searcher, WithDownloadProber(prober))

	ac := &AcquisitionContext{
		Track: TrackRef{Title: "How Sweet", Artist: "NewJeans", Duration: 226},
		Ranked: []ports.AudioCandidate{
			{URL: "https://youtube.com/watch?v=bloated", Duration: 0},
			{URL: "https://youtube.com/watch?v=correct", Duration: 227},
		},
	}

	if _, err := step.Execute(context.Background(), ac, afterSelect{}); err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(ac.TempPath))

	if prober.calls != 2 {
		t.Errorf("expected both candidates probed, got %d probe calls", prober.calls)
	}
	if ac.Selected == nil || ac.Selected.URL != "https://youtube.com/watch?v=correct" {
		t.Fatalf("expected the correct-length candidate selected, got %+v", ac.Selected)
	}
	if ac.TempPath == "" {
		t.Error("expected TempPath set for the accepted candidate")
	}
}

func TestDownloadStep_AllCandidatesWrongDuration_Errors(t *testing.T) {
	searcher := &fileWritingSearcher{writeFile: true}
	prober := &queueProber{durations: []float64{840}}
	step := NewDownloadStep(searcher, WithDownloadProber(prober))

	ac := &AcquisitionContext{
		Track:  TrackRef{Title: "How Sweet", Artist: "NewJeans", Duration: 226},
		Ranked: []ports.AudioCandidate{{URL: "https://youtube.com/watch?v=bloated", Duration: 840}},
	}

	if _, err := step.Execute(context.Background(), ac, afterSelect{}); err == nil {
		t.Fatal("expected an error when no candidate matches the expected duration")
	}
	if ac.TempPath != "" {
		t.Errorf("TempPath must stay empty when all candidates are rejected, got %q", ac.TempPath)
	}
}

func TestDownloadStep_NoExpectedDuration_SkipsVerification(t *testing.T) {
	searcher := &fileWritingSearcher{writeFile: true}
	prober := &queueProber{durations: []float64{840}}
	step := NewDownloadStep(searcher, WithDownloadProber(prober))

	ac := &AcquisitionContext{
		Track:  TrackRef{Title: "Unknown", Artist: "Artist", Duration: 0},
		Ranked: []ports.AudioCandidate{{URL: "https://youtube.com/watch?v=whatever", Duration: 840}},
	}

	if _, err := step.Execute(context.Background(), ac, afterSelect{}); err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(ac.TempPath))

	if prober.calls != 0 {
		t.Errorf("expected no probing when expected duration is unknown, got %d calls", prober.calls)
	}
	if ac.TempPath == "" {
		t.Error("expected the first candidate accepted unverified")
	}
}

func TestDownloadStep_RejectsUndecodableAudio(t *testing.T) {
	searcher := &fileWritingSearcher{writeFile: true}
	prober := &queueProber{
		durations:  []float64{226, 226},
		decodeErrs: []error{errors.New("audio stream failed to decode"), nil},
	}
	step := NewDownloadStep(searcher, WithDownloadProber(prober))

	ac := &AcquisitionContext{
		Track: TrackRef{Title: "X", Artist: "Y", Duration: 226},
		Ranked: []ports.AudioCandidate{
			{URL: "https://youtube.com/watch?v=corrupt", Duration: 226},
			{URL: "https://youtube.com/watch?v=good", Duration: 226},
		},
	}

	if _, err := step.Execute(context.Background(), ac, afterSelect{}); err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(ac.TempPath))

	if ac.Selected == nil || ac.Selected.URL != "https://youtube.com/watch?v=good" {
		t.Fatalf("expected corrupt candidate rejected and decodable one accepted, got %+v", ac.Selected)
	}
}

type cancellingProber struct{ cancel context.CancelFunc }

func (p cancellingProber) ProbeDuration(_ context.Context, _ string) (float64, error) {
	return 226, nil
}

func (p cancellingProber) ValidateDecodable(_ context.Context, _ string) error {
	p.cancel()
	return context.Canceled
}

func TestDownloadStep_DecodeCancelled_RecordsNoLastingRejection(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	step := NewDownloadStep(&fileWritingSearcher{writeFile: true}, WithDownloadProber(cancellingProber{cancel: cancel}))
	ac := &AcquisitionContext{
		Track:  TrackRef{ID: "t1", Title: "X", Artist: "Y", Duration: 226},
		Ranked: []ports.AudioCandidate{{URL: "https://youtube.com/watch?v=abc", Duration: 226}},
	}

	_, err := step.Execute(ctx, ac, afterSelect{})

	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("Execute error = %v, want one wrapping context.Canceled", err)
	}
	if recs := lastingRejectionRecords(ac); len(recs) != 0 {
		t.Errorf("lasting rejection records = %+v, want none for a cancelled decode", recs)
	}
}

func TestDownloadStep_DecodeFailureWithLiveContext_RecordsUndecodable(t *testing.T) {
	prober := &queueProber{
		durations:  []float64{226},
		decodeErrs: []error{errors.New("audio stream failed to decode")},
	}
	step := NewDownloadStep(&fileWritingSearcher{writeFile: true}, WithDownloadProber(prober))
	ac := &AcquisitionContext{
		Track:  TrackRef{ID: "t1", Title: "X", Artist: "Y", Duration: 226},
		Ranked: []ports.AudioCandidate{{URL: "https://youtube.com/watch?v=abc", Duration: 226}},
	}

	if _, err := step.Execute(context.Background(), ac, afterSelect{}); err == nil {
		t.Fatal("expected an error when the only candidate is undecodable")
	}
	recs := lastingRejectionRecords(ac)
	if len(recs) != 1 || recs[0].Reason != string(RejectionUndecodable) {
		t.Errorf("lasting rejection records = %+v, want one undecodable", recs)
	}
}

func TestDownloadStep_AllUndecodable_Errors(t *testing.T) {
	searcher := &fileWritingSearcher{writeFile: true}
	prober := &queueProber{
		durations:  []float64{226},
		decodeErrs: []error{errors.New("audio stream failed to decode")},
	}
	step := NewDownloadStep(searcher, WithDownloadProber(prober))

	ac := &AcquisitionContext{
		Track:  TrackRef{Title: "X", Artist: "Y", Duration: 226},
		Ranked: []ports.AudioCandidate{{URL: "https://youtube.com/watch?v=corrupt", Duration: 226}},
	}

	if _, err := step.Execute(context.Background(), ac, afterSelect{}); err == nil {
		t.Fatal("expected an error when the only candidate is undecodable")
	}
	if ac.TempPath != "" {
		t.Errorf("TempPath must stay empty when the candidate is rejected, got %q", ac.TempPath)
	}
}

func TestRankCandidates_TopicFirstThenOrdered(t *testing.T) {
	track := TrackRef{Title: "How Sweet", Artist: "NewJeans", Duration: 226}
	candidates := []ports.AudioCandidate{
		{Title: "How Sweet", Channel: "HALLYUSOUND", Duration: 227, URL: "other", Categories: []string{"Music"}, ViewCount: 1000},
		{Title: "How Sweet", Channel: "NewJeans - Topic", Duration: 840, URL: "topic", Categories: []string{"Music"}, ViewCount: 9_000_000},
	}

	ranked, _ := rankAndCollect(context.Background(), track, candidates)
	if len(ranked) != 2 {
		t.Fatalf("expected both candidates ranked, got %d", len(ranked))
	}
	if ranked[0].URL != "topic" {
		t.Errorf("expected the Topic candidate ranked first, got %q", ranked[0].URL)
	}
	if ranked[1].URL != "other" {
		t.Errorf("expected the non-Topic candidate ranked second, got %q", ranked[1].URL)
	}
}

const (
	logTestTrackID = "11111111-2222-3333-4444-555555555555"
	logTestSource  = "soundcloud"
)

type scriptedProber struct {
	duration  float64
	probeErr  error
	decodeErr error
}

func (p scriptedProber) ProbeDuration(context.Context, string) (float64, error) {
	return p.duration, p.probeErr
}

func (p scriptedProber) ValidateDecodable(context.Context, string) error { return p.decodeErr }

func captureJSONLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func findLogRecord(t *testing.T, buf *bytes.Buffer, want string) map[string]any {
	t.Helper()
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("unparseable log line %q: %v", line, err)
		}
		if rec["msg"] == want {
			return rec
		}
	}
	t.Fatalf("log %q not emitted; got:\n%s", want, buf.String())
	return nil
}

func logTestCandidate() ports.AudioCandidate {
	return ports.AudioCandidate{
		URL:    "https://soundcloud.com/artist/song",
		Title:  "Artist - Song",
		Source: logTestSource,
	}
}

func logTestDownloadContext() *AcquisitionContext {
	return &AcquisitionContext{
		Track:  TrackRef{ID: logTestTrackID, Title: "Song", Artist: "Artist", Duration: 200},
		Ranked: []ports.AudioCandidate{logTestCandidate()},
	}
}

func TestCandidateLogs_StampTrackIDAndSource(t *testing.T) {
	identityCtx := func(cluster []string) *AcquisitionContext {
		ac := logTestDownloadContext()
		ac.Track.Duration = 0
		ac.Identity = ports.RecordingIdentity{MBID: "mb-want", AcoustIDs: cluster}
		return ac
	}

	tests := []struct {
		name string
		msg  string
		run  func(ctx context.Context, t *testing.T)
	}{
		{
			name: "search exclusion",
			msg:  "acquisition.candidate_excluded",
			run: func(ctx context.Context, t *testing.T) {
				c := logTestCandidate()
				other := ports.AudioCandidate{URL: "https://soundcloud.com/artist/other", Source: logTestSource}
				step := NewSearchStep(&fakeAudioSearcher{searchResults: []ports.AudioCandidate{c, other}})
				ac := &AcquisitionContext{
					Track:   TrackRef{ID: logTestTrackID, Title: "Song", Artist: "Artist"},
					Replace: ReplaceState{ExcludeKeys: []string{sourceKey(c.URL)}},
				}
				_, _ = step.Execute(ctx, ac, pipelineStart{})
			},
		},
		{
			name: "candidate evaluation",
			msg:  "candidate_evaluated",
			run: func(ctx context.Context, t *testing.T) {
				track := TrackRef{ID: logTestTrackID, Title: "Song", Artist: "Artist"}
				_, _ = rankAndCollect(ctx, track, []ports.AudioCandidate{logTestCandidate()})
			},
		},
		{
			name: "download failure",
			msg:  "acquisition.candidate_download_failed",
			run: func(ctx context.Context, t *testing.T) {
				step := NewDownloadStep(&fileWritingSearcher{err: errors.New("fetch boom")})
				_, _ = step.Execute(ctx, logTestDownloadContext(), afterSelect{})
			},
		},
		{
			name: "probe failure",
			msg:  "acquisition.probe_failed_accepting",
			run: func(ctx context.Context, t *testing.T) {
				runDownloadForLog(ctx, t, logTestDownloadContext(),
					WithDownloadProber(scriptedProber{probeErr: errors.New("probe boom")}))
			},
		},
		{
			name: "duration rejection",
			msg:  "acquisition.candidate_rejected_duration",
			run: func(ctx context.Context, t *testing.T) {
				runDownloadForLog(ctx, t, logTestDownloadContext(),
					WithDownloadProber(scriptedProber{duration: 30}))
			},
		},
		{
			name: "undecodable rejection",
			msg:  "acquisition.candidate_rejected_undecodable",
			run: func(ctx context.Context, t *testing.T) {
				runDownloadForLog(ctx, t, logTestDownloadContext(),
					WithDownloadProber(scriptedProber{duration: 200, decodeErr: errors.New("decode boom")}))
			},
		},
		{
			name: "identify failure",
			msg:  "acquisition.identify_failed",
			run: func(ctx context.Context, t *testing.T) {
				runDownloadForLog(ctx, t, identityCtx(nil),
					WithDownloadIdentifier(&stubIdentifier{err: errors.New("acoustid boom")}))
			},
		},
		{
			name: "identify unknown",
			msg:  "acquisition.identify_unknown",
			run: func(ctx context.Context, t *testing.T) {
				runDownloadForLog(ctx, t, identityCtx(nil),
					WithDownloadIdentifier(&stubIdentifier{}))
			},
		},
		{
			name: "identify uncorroborated",
			msg:  "acquisition.identify_uncorroborated",
			run: func(ctx context.Context, t *testing.T) {
				runDownloadForLog(ctx, t, identityCtx(nil),
					WithDownloadIdentifier(&stubIdentifier{match: ports.RecordingMatch{MBIDs: []string{"mb-other"}}}))
			},
		},
		{
			name: "fingerprint rejection",
			msg:  "acquisition.candidate_rejected_fingerprint",
			run: func(ctx context.Context, t *testing.T) {
				runDownloadForLog(ctx, t, identityCtx([]string{"ac-want"}),
					WithDownloadIdentifier(&stubIdentifier{match: ports.RecordingMatch{AcoustID: "ac-other", MBIDs: []string{"mb-other"}}}))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs := captureJSONLog(t)
			tt.run(context.Background(), t)

			rec := findLogRecord(t, logs, tt.msg)
			if got := rec["track_id"]; got != logTestTrackID {
				t.Errorf("%s track_id = %v, want %q; record: %v", tt.msg, got, logTestTrackID, rec)
			}
			if got := rec["source"]; got != logTestSource {
				t.Errorf("%s source = %v, want %q; record: %v", tt.msg, got, logTestSource, rec)
			}
		})
	}
}

func runDownloadForLog(ctx context.Context, t *testing.T, ac *AcquisitionContext, opts ...func(*DownloadStep)) {
	t.Helper()
	step := NewDownloadStep(&fileWritingSearcher{writeFile: true}, opts...)
	_, _ = step.Execute(ctx, ac, afterSelect{})
	if ac.TempPath != "" {
		_ = os.RemoveAll(filepath.Dir(ac.TempPath))
	}
}

type stubIdentifier struct {
	match   ports.RecordingMatch
	err     error
	cluster []string
	calls   int
}

func (s *stubIdentifier) Identify(context.Context, string, float64) (ports.RecordingMatch, error) {
	s.calls++
	return s.match, s.err
}

func (s *stubIdentifier) AcoustIDsFor(context.Context, string) ([]string, error) {
	return s.cluster, nil
}

func downloadContext(mbid string, cluster []string) *AcquisitionContext {
	return &AcquisitionContext{
		Track:    TrackRef{Title: "Sunglasses at Night", Artist: "Corey Hart"},
		Identity: ports.RecordingIdentity{MBID: mbid, AcoustIDs: cluster},
		Ranked: []ports.AudioCandidate{
			{URL: "https://youtube.com/watch?v=first000000"},
			{URL: "https://youtube.com/watch?v=second00000"},
		},
	}
}

func TestDownloadStep_RejectsAudioOutsideTheExpectedCluster(t *testing.T) {
	identifier := &stubIdentifier{
		match:   ports.RecordingMatch{AcoustID: "ac-acoustic", MBIDs: []string{"mb-acoustic"}},
		cluster: []string{"ac-master-a", "ac-master-b"},
	}
	step := NewDownloadStep(&fileWritingSearcher{writeFile: true}, WithDownloadIdentifier(identifier))
	ac := downloadContext("mb-master", []string{"ac-master-a", "ac-master-b"})

	if _, err := step.Execute(context.Background(), ac, afterSelect{}); err == nil {
		t.Fatal("expected every candidate rejected: the audio is a different recording")
	}
	if ac.TempPath != "" {
		t.Errorf("TempPath must stay empty when the fingerprint rejects, got %q", ac.TempPath)
	}
}

func TestDownloadStep_AcceptsAudioInsideTheExpectedCluster(t *testing.T) {
	identifier := &stubIdentifier{
		match: ports.RecordingMatch{AcoustID: "ac-master-b", MBIDs: []string{"mb-some-other-release"}},
	}
	step := NewDownloadStep(&fileWritingSearcher{writeFile: true}, WithDownloadIdentifier(identifier))
	ac := downloadContext("mb-master", []string{"ac-master-a", "ac-master-b"})

	if _, err := step.Execute(context.Background(), ac, afterSelect{}); err != nil {
		t.Fatalf("the Don't Stop the Music case: MBIDs need not intersect, the cluster settles it — got %v", err)
	}
	defer os.RemoveAll(filepath.Dir(ac.TempPath))

	if !ac.IdentityVerified {
		t.Error("a cluster hit is the strongest claim available and must mark the track verified")
	}
}

func TestDownloadStep_NeverRejectsWithoutAnExpectedCluster(t *testing.T) {
	identifier := &stubIdentifier{
		match: ports.RecordingMatch{AcoustID: "ac-unrelated", MBIDs: []string{"mb-unrelated"}},
	}
	step := NewDownloadStep(&fileWritingSearcher{writeFile: true}, WithDownloadIdentifier(identifier))
	ac := downloadContext("mb-master", nil)
	ac.Track.Duration = 236
	ac.Ranked = []ports.AudioCandidate{{URL: "https://youtube.com/watch?v=first000000", Channel: "Corey Hart", Duration: 236}}

	if _, err := step.Execute(context.Background(), ac, afterSelect{}); err != nil {
		t.Fatalf("with no ground truth there is nothing to reject against; the long tail must stay acquirable: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(ac.TempPath))

	if ac.IdentityVerified {
		t.Error("accepting without evidence must not claim verification")
	}
	if ac.Provenance() != domain.ProvenanceBestEffort {
		t.Errorf("provenance = %q, want best_effort", ac.Provenance())
	}
}

func TestDownloadStep_UnknownAudioIsAccepted(t *testing.T) {
	identifier := &stubIdentifier{cluster: []string{"ac-master"}}
	step := NewDownloadStep(&fileWritingSearcher{writeFile: true}, WithDownloadIdentifier(identifier))
	ac := downloadContext("mb-master", []string{"ac-master"})
	ac.Track.Duration = 236
	ac.Ranked = []ports.AudioCandidate{{URL: "https://youtube.com/watch?v=first000000", Channel: "Corey Hart", Duration: 236}}

	if _, err := step.Execute(context.Background(), ac, afterSelect{}); err != nil {
		t.Fatalf("AcoustID coverage is crowd-sourced; unknown audio must be accepted: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(ac.TempPath))

	if ac.Provenance() != domain.ProvenanceBestEffort {
		t.Errorf("provenance = %q, want best_effort", ac.Provenance())
	}
}

func TestDownloadStep_IdentifierErrorIsFailOpen(t *testing.T) {
	identifier := &stubIdentifier{err: errors.New("acoustid down"), cluster: []string{"ac-master"}}
	step := NewDownloadStep(&fileWritingSearcher{writeFile: true}, WithDownloadIdentifier(identifier))
	ac := downloadContext("mb-master", []string{"ac-master"})

	if _, err := step.Execute(context.Background(), ac, afterSelect{}); err != nil {
		t.Fatalf("acquisition must never block on a broken validator: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(ac.TempPath))
}

func TestDownloadStep_NoExpectedMBIDSkipsIdentificationEntirely(t *testing.T) {
	identifier := &stubIdentifier{match: ports.RecordingMatch{AcoustID: "ac-whatever"}}
	step := NewDownloadStep(&fileWritingSearcher{writeFile: true}, WithDownloadIdentifier(identifier))
	ac := downloadContext("", []string{"ac-master"})

	if _, err := step.Execute(context.Background(), ac, afterSelect{}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(ac.TempPath))

	if identifier.calls != 0 {
		t.Errorf("identify calls = %d, want 0 with nothing to compare against", identifier.calls)
	}
}

func TestDownloadStep_RejectionWalksToTheNextCandidate(t *testing.T) {
	identifier := &rejectFirstIdentifier{cluster: []string{"ac-master"}}
	step := NewDownloadStep(&fileWritingSearcher{writeFile: true}, WithDownloadIdentifier(identifier))
	ac := downloadContext("mb-master", []string{"ac-master"})

	if _, err := step.Execute(context.Background(), ac, afterSelect{}); err != nil {
		t.Fatalf("a rejected candidate must not end the walk: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(ac.TempPath))

	if ac.Selected == nil || ac.Selected.URL != "https://youtube.com/watch?v=second00000" {
		t.Fatalf("selected = %+v, want the second candidate", ac.Selected)
	}
}

type rejectFirstIdentifier struct {
	cluster []string
	calls   int
}

func (r *rejectFirstIdentifier) Identify(context.Context, string, float64) (ports.RecordingMatch, error) {
	r.calls++
	if r.calls == 1 {
		return ports.RecordingMatch{AcoustID: "ac-wrong", MBIDs: []string{"mb-wrong"}}, nil
	}
	return ports.RecordingMatch{AcoustID: "ac-master", MBIDs: []string{"mb-master"}}, nil
}

func (r *rejectFirstIdentifier) AcoustIDsFor(context.Context, string) ([]string, error) {
	return r.cluster, nil
}

func linkedResults(recordings ...ports.LinkedRecording) []ports.AcoustIDResult {
	return []ports.AcoustIDResult{{ID: "ac-audio", Score: 0.97, Recordings: recordings}}
}

func TestDownloadStep_LiveOnlyLinksRejectedThenCleanCandidateVerified(t *testing.T) {
	live := ports.RecordingMatch{
		AcoustID: "ac-live", MBIDs: []string{"mb-live"}, Score: 0.97,
		Results: linkedResults(ports.LinkedRecording{MBID: "mb-live", Title: "Nessun dorma (live)"}),
	}
	clean := ports.RecordingMatch{
		AcoustID: "ac-studio", MBIDs: []string{"mb-studio"}, Score: 0.98,
		Results: linkedResults(ports.LinkedRecording{MBID: "mb-studio", Title: "Nessun dorma"}),
	}
	identifier := &sequenceIdentifier{matches: []ports.RecordingMatch{live, clean}}
	step := NewDownloadStep(&fileWritingSearcher{writeFile: true}, WithDownloadIdentifier(identifier))
	ac := downloadContext("mb-studio", nil)
	ac.Track.Title = "Nessun dorma"

	if _, err := step.Execute(context.Background(), ac, afterSelect{}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(ac.TempPath))

	if len(ac.Rejections) != 1 || ac.Rejections[0].Stage != RejectionFingerprint || ac.Rejections[0].Reason != "other version" {
		t.Errorf("rejections = %+v, want one fingerprint rejection with reason other version", ac.Rejections)
	}
	if ac.Provenance() != domain.ProvenanceVerified || ac.Verdict.Kind != VerdictHard {
		t.Errorf("provenance = %v, verdict = %v, want verified and hard", ac.Provenance(), ac.Verdict.Kind)
	}
}

func TestDownloadStep_LinksToAnotherSongAreRejected(t *testing.T) {
	match := ports.RecordingMatch{
		AcoustID: "ac-x", MBIDs: []string{"mb-x"}, Score: 0.9,
		Results: linkedResults(ports.LinkedRecording{MBID: "mb-x", Title: "Something Else Entirely"}),
	}
	step := NewDownloadStep(&fileWritingSearcher{writeFile: true}, WithDownloadIdentifier(&stubIdentifier{match: match}))
	ac := downloadContext("mb-master", nil)

	if _, err := step.Execute(context.Background(), ac, afterSelect{}); err == nil {
		t.Fatal("expected every candidate rejected as a different song")
	}
	if len(ac.Rejections) == 0 || ac.Rejections[0].Reason != "different song" {
		t.Errorf("rejections = %+v, want reason different song", ac.Rejections)
	}
}

func TestDownloadStep_LogsAudioVerdictForEveryDecision(t *testing.T) {
	logs := captureJSONLog(t)
	match := ports.RecordingMatch{
		AcoustID: "ac-a", MBIDs: []string{"mb-master"}, Score: 0.97,
		Results: linkedResults(ports.LinkedRecording{MBID: "mb-master", Title: "Sunglasses at Night"}),
	}
	step := NewDownloadStep(&fileWritingSearcher{writeFile: true}, WithDownloadIdentifier(&stubIdentifier{match: match}))
	ac := downloadContext("mb-master", nil)

	if _, err := step.Execute(context.Background(), ac, afterSelect{}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(ac.TempPath))

	rec := findLogRecord(t, logs, "acquisition.audio_verdict")
	if rec["verdict"] != "hard" || rec["score"] != 0.97 {
		t.Errorf("audio_verdict record = %v, want verdict hard, score 0.97", rec)
	}
}

type sequenceIdentifier struct {
	matches []ports.RecordingMatch
	calls   int
}

func (s *sequenceIdentifier) Identify(context.Context, string, float64) (ports.RecordingMatch, error) {
	match := s.matches[min(s.calls, len(s.matches)-1)]
	s.calls++
	return match, nil
}

func (s *sequenceIdentifier) AcoustIDsFor(context.Context, string) ([]string, error) {
	return nil, nil
}

type previewingFetcher struct {
	fileWritingSearcher
	previewErr  error
	previewDirs []string
}

func (f *previewingFetcher) PreviewFetcherFor(ports.AudioCandidate) (ports.PreviewFetcher, bool) {
	return f, true
}

func (f *previewingFetcher) FetchPreview(_ context.Context, _ ports.AudioCandidate, outDir string, _ int) (string, error) {
	f.previewDirs = append(f.previewDirs, outDir)
	if f.previewErr != nil {
		return "", f.previewErr
	}
	path := filepath.Join(outDir, "preview.mp3")
	return path, os.WriteFile(path, []byte("preview-bytes"), 0o644)
}

type durationRecordingIdentifier struct {
	match     ports.RecordingMatch
	durations []float64
}

func (d *durationRecordingIdentifier) Identify(_ context.Context, _ string, hint float64) (ports.RecordingMatch, error) {
	d.durations = append(d.durations, hint)
	return d.match, nil
}

func (d *durationRecordingIdentifier) AcoustIDsFor(context.Context, string) ([]string, error) {
	return nil, nil
}

func previewMatch(mbid, title string) ports.RecordingMatch {
	return ports.RecordingMatch{
		AcoustID: "ac-" + mbid, MBIDs: []string{mbid}, Score: 0.98,
		Results: linkedResults(ports.LinkedRecording{MBID: mbid, Title: title}),
	}
}

func previewContext() *AcquisitionContext {
	ac := downloadContext("mb-studio", nil)
	ac.Track.Title = "Nessun dorma"
	ac.Ranked = ac.Ranked[:1]
	ac.Ranked[0].Duration = 250
	return ac
}

func TestDownloadStep_PreviewOtherVersionRejectsWithoutFullFetch(t *testing.T) {
	fetcher := &previewingFetcher{fileWritingSearcher: fileWritingSearcher{writeFile: true}}
	identifier := &durationRecordingIdentifier{match: previewMatch("mb-live", "Nessun dorma (live)")}
	step := NewDownloadStep(fetcher, WithDownloadIdentifier(identifier))
	ac := previewContext()

	if _, err := step.Execute(context.Background(), ac, afterSelect{}); err == nil {
		t.Fatal("expected the live preview to reject the only candidate")
	}

	if fetcher.calls != 0 {
		t.Errorf("full fetches = %d, want 0 after a rejecting preview", fetcher.calls)
	}
	if len(ac.Rejections) != 1 || ac.Rejections[0].Reason != "other version" {
		t.Errorf("rejections = %+v, want one other version rejection", ac.Rejections)
	}
}

func TestDownloadStep_AcceptedPreviewIsFollowedByOneFullFetchAndNoSecondIdentify(t *testing.T) {
	fetcher := &previewingFetcher{fileWritingSearcher: fileWritingSearcher{writeFile: true}}
	identifier := &durationRecordingIdentifier{match: previewMatch("mb-studio", "Nessun dorma")}
	step := NewDownloadStep(fetcher, WithDownloadIdentifier(identifier))
	ac := previewContext()

	if _, err := step.Execute(context.Background(), ac, afterSelect{}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(ac.TempPath))

	if fetcher.calls != 1 || len(identifier.durations) != 1 {
		t.Errorf("full fetches = %d, identify calls = %d, want 1 and 1", fetcher.calls, len(identifier.durations))
	}
	if ac.Verdict.Kind != VerdictHard || !ac.IdentityVerified {
		t.Errorf("verdict = %q identity = %v, want the preview's hard verdict carried over", ac.Verdict.Kind, ac.IdentityVerified)
	}
	if _, err := os.Stat(fetcher.previewDirs[0]); !os.IsNotExist(err) {
		t.Errorf("preview dir still present: %v", err)
	}
}

func TestDownloadStep_FailedPreviewFallsBackToTheFullPath(t *testing.T) {
	fetcher := &previewingFetcher{fileWritingSearcher: fileWritingSearcher{writeFile: true}, previewErr: errors.New("boom")}
	identifier := &durationRecordingIdentifier{match: previewMatch("mb-studio", "Nessun dorma")}
	step := NewDownloadStep(fetcher, WithDownloadIdentifier(identifier))
	ac := previewContext()

	if _, err := step.Execute(context.Background(), ac, afterSelect{}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(ac.TempPath))

	if fetcher.calls != 1 || len(identifier.durations) != 1 {
		t.Errorf("full fetches = %d, identify calls = %d, want the full path to run once", fetcher.calls, len(identifier.durations))
	}
}

func TestDownloadStep_PreviewIdentifyCarriesTheCandidatesFullDuration(t *testing.T) {
	fetcher := &previewingFetcher{fileWritingSearcher: fileWritingSearcher{writeFile: true}}
	identifier := &durationRecordingIdentifier{match: previewMatch("mb-studio", "Nessun dorma")}
	step := NewDownloadStep(fetcher, WithDownloadIdentifier(identifier))
	ac := previewContext()

	if _, err := step.Execute(context.Background(), ac, afterSelect{}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(ac.TempPath))

	if len(identifier.durations) != 1 || identifier.durations[0] != 250 {
		t.Errorf("identify duration hints = %v, want [250]", identifier.durations)
	}
}

type gatedFetcher struct {
	mu       sync.Mutex
	dirs     map[string]string
	gates    map[string]chan struct{}
	started  chan string
	inFlight atomic.Int32
	peak     atomic.Int32
}

func newGatedFetcher(gates map[string]chan struct{}) *gatedFetcher {
	return &gatedFetcher{dirs: map[string]string{}, gates: gates, started: make(chan string, 64)}
}

func (f *gatedFetcher) Fetch(_ context.Context, c ports.AudioCandidate, outDir string) (string, error) {
	now := f.inFlight.Add(1)
	defer f.inFlight.Add(-1)
	for {
		peak := f.peak.Load()
		if now <= peak || f.peak.CompareAndSwap(peak, now) {
			break
		}
	}
	f.mu.Lock()
	f.dirs[c.URL] = outDir
	f.mu.Unlock()
	f.started <- c.URL
	if gate := f.gates[c.URL]; gate != nil {
		<-gate
	}
	path := filepath.Join(outDir, "track.mp3")
	return path, os.WriteFile(path, []byte("audio"), 0o644)
}

func (f *gatedFetcher) dirOf(url string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.dirs[url]
}

func TestDownloadStep_WindowedKeepsBestRankedAcceptedEvenWhenLowerRankFinishesFirst(t *testing.T) {
	rank1Gate := make(chan struct{})
	fetcher := newGatedFetcher(map[string]chan struct{}{"rank1": rank1Gate})
	ac := &AcquisitionContext{Ranked: []ports.AudioCandidate{{URL: "rank1"}, {URL: "rank2"}}}
	done := make(chan error, 1)

	go func() {
		_, err := NewDownloadStep(fetcher, WithVerifyWidth(2)).Execute(context.Background(), ac, afterSelect{})
		done <- err
	}()
	for range 2 {
		<-fetcher.started
	}
	for fetcher.inFlight.Load() != 1 {
		time.Sleep(time.Millisecond)
	}
	close(rank1Gate)

	if err := <-done; err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(ac.TempPath))
	if ac.Selected == nil || ac.Selected.URL != "rank1" {
		t.Fatalf("selected = %+v, want rank1", ac.Selected)
	}
	if _, err := os.Stat(fetcher.dirOf("rank2")); !os.IsNotExist(err) {
		t.Errorf("losing accepted attempt's temp dir should be removed, stat err = %v", err)
	}
}

func TestDownloadStep_SharedLimiterCapsInFlightFetchesAcrossExecutes(t *testing.T) {
	gate := make(chan struct{})
	fetcher := newGatedFetcher(map[string]chan struct{}{"a": gate, "b": gate})
	limiter := NewDownloadLimiter(6)
	var wg sync.WaitGroup
	execute := func(step *DownloadStep, urls ...string) {
		defer wg.Done()
		ranked := make([]ports.AudioCandidate, 0, len(urls))
		for _, u := range urls {
			ranked = append(ranked, ports.AudioCandidate{URL: u})
		}
		ac := &AcquisitionContext{Ranked: ranked}
		if _, err := step.Execute(context.Background(), ac, afterSelect{}); err == nil {
			os.RemoveAll(filepath.Dir(ac.TempPath))
		}
	}

	wg.Add(3)
	for range 3 {
		go execute(NewDownloadStep(fetcher, WithVerifyWidth(2), WithStepDownloadLimiter(limiter)), "a", "b")
	}
	for range 6 {
		<-fetcher.started
	}
	wg.Add(1)
	go execute(NewDownloadStep(fetcher, WithStepDownloadLimiter(limiter)), "seventh")

	select {
	case <-fetcher.started:
		t.Fatal("seventh fetch started while six were in flight")
	case <-time.After(50 * time.Millisecond):
	}
	close(gate)
	wg.Wait()

	if peak := fetcher.peak.Load(); peak > 6 {
		t.Errorf("peak in-flight fetches = %d, want <= 6", peak)
	}
}

type gateRecorder struct{ gates []string }

func (r *gateRecorder) RecordVerifySkip(gate string) { r.gates = append(r.gates, gate) }

func TestDownloadStep_IdentifierErrorRecordsAnIdentifyFailedSkip(t *testing.T) {
	rec := &gateRecorder{}
	identifier := &stubIdentifier{err: errors.New("acoustid down"), cluster: []string{"ac-master"}}
	step := NewDownloadStep(&fileWritingSearcher{writeFile: true},
		WithDownloadIdentifier(identifier), WithStepVerifySkips(rec))
	ac := downloadContext("mb-master", []string{"ac-master"})

	if _, err := step.Execute(context.Background(), ac, afterSelect{}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(ac.TempPath))

	if len(rec.gates) != 1 || rec.gates[0] != "identify_failed" {
		t.Errorf("recorded gates = %v, want [identify_failed]", rec.gates)
	}
}

func TestDownloadStep_FailedPreviewIdentifyRecordsAPreviewFallbackSkip(t *testing.T) {
	rec := &gateRecorder{}
	fetcher := &previewingFetcher{fileWritingSearcher: fileWritingSearcher{writeFile: true}, previewErr: errors.New("boom")}
	identifier := &durationRecordingIdentifier{match: previewMatch("mb-studio", "Nessun dorma")}
	step := NewDownloadStep(fetcher, WithDownloadIdentifier(identifier), WithStepVerifySkips(rec))
	ac := previewContext()

	if _, err := step.Execute(context.Background(), ac, afterSelect{}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(ac.TempPath))

	if len(rec.gates) != 1 || rec.gates[0] != "preview_fallback" {
		t.Errorf("recorded gates = %v, want [preview_fallback]", rec.gates)
	}
}

func TestDownloadStep_ThrottledIdentifyLogsThrottledAndLeavesIdentityUnverified(t *testing.T) {
	logs := captureJSONLog(t)
	ac := logTestDownloadContext()
	ac.Track.Duration = 0
	ac.Identity = ports.RecordingIdentity{MBID: "mb-want"}
	throttled := fmt.Errorf("acoustid lookup: %w", ports.ErrIdentifyThrottled)

	runDownloadForLog(context.Background(), t, ac,
		WithDownloadIdentifier(&stubIdentifier{err: throttled}))

	findLogRecord(t, logs, "acquisition.identify_throttled")
	if strings.Contains(logs.String(), "acquisition.identify_failed") {
		t.Errorf("throttle logged as identify_failed: %s", logs.String())
	}
	if ac.IdentityVerified {
		t.Error("IdentityVerified = true, want false after a throttled lookup")
	}
}

func TestDownloadStep_ThrottledPreviewLogsThrottledNotPreviewFallback(t *testing.T) {
	logs := captureJSONLog(t)
	fetcher := &previewingFetcher{fileWritingSearcher: fileWritingSearcher{writeFile: true}}
	identifier := &stubIdentifier{err: fmt.Errorf("acoustid lookup: %w", ports.ErrIdentifyThrottled)}
	step := NewDownloadStep(fetcher, WithDownloadIdentifier(identifier))
	ac := previewContext()

	_, _ = step.Execute(context.Background(), ac, afterSelect{})
	if ac.TempPath != "" {
		_ = os.RemoveAll(filepath.Dir(ac.TempPath))
	}

	findLogRecord(t, logs, "acquisition.identify_throttled")
	if strings.Contains(logs.String(), "acquisition.preview_fallback") {
		t.Errorf("throttle logged as preview_fallback: %s", logs.String())
	}
}

type nestedLayoutFetcher struct{}

func (nestedLayoutFetcher) Fetch(_ context.Context, _ ports.AudioCandidate, outDir string) (string, error) {
	dir := filepath.Join(outDir, "Artist", "Album")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "track.flac")
	return path, os.WriteFile(path, []byte("audio-bytes"), 0o644)
}

func TestDownloadStep_Execute_NestedLayout_RollbackLeavesNoTempRoot(t *testing.T) {
	step := NewDownloadStep(nestedLayoutFetcher{})
	ac := &AcquisitionContext{Ranked: []ports.AudioCandidate{{URL: "https://example.com/x"}}}

	if _, err := step.Execute(context.Background(), ac, afterSelect{}); err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	root := ac.TempDir
	if !strings.HasPrefix(filepath.Base(root), tempDirPrefix) || filepath.Dir(root) != os.TempDir() {
		t.Fatalf("TempDir = %q, want the %s* MkdirTemp root", root, tempDirPrefix)
	}

	if err := step.Rollback(context.Background(), ac); err != nil {
		t.Fatalf("Rollback error: %v", err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Errorf("temp root %q should be gone after rollback, stat err = %v", root, err)
	}
}

type findOnlySource struct {
	name       string
	candidates []ports.AudioCandidate
	err        error
	fetchErr   error
}

func (s findOnlySource) Name() string { return s.name }

func (s findOnlySource) Find(context.Context, ports.FindRequest) ([]ports.AudioCandidate, error) {
	return s.candidates, s.err
}

func (s findOnlySource) Fetch(context.Context, ports.AudioCandidate, string) (string, error) {
	return "", s.fetchErr
}

func searchThenDownloadRejectingAll(t *testing.T, down findOnlySource) (context.Context, error) {
	t.Helper()
	healthy := findOnlySource{
		name:       "healthy",
		candidates: []ports.AudioCandidate{{URL: "https://example.com/a"}},
		fetchErr:   errors.New("exit 1"),
	}
	registry := NewSourceRegistry(down, healthy)
	ctx := withJobAttempt(context.Background(), 1)
	ac := &AcquisitionContext{}
	if _, err := NewSearchStep(registry).Execute(ctx, ac, pipelineStart{}); err != nil {
		t.Fatalf("search: %v", err)
	}
	ac.Ranked = ac.Candidates
	_, err := NewDownloadStep(registry).Execute(ctx, ac, afterSelect{})
	if err == nil {
		t.Fatal("expected a download error")
	}
	return ctx, err
}

func TestDownloadStep_SearchOutageWithAllSurvivorsRejected_IsRetryable(t *testing.T) {
	down := findOnlySource{name: "down", err: &ports.SourceUnavailableError{Source: "down", Err: errors.New("429")}}

	ctx, err := searchThenDownloadRejectingAll(t, down)

	if !ports.IsSourceUnavailable(err) || !willRetry(ctx, err) {
		t.Fatalf("err = %v, want a retryable source-unavailable failure", err)
	}
}

func TestDownloadStep_HealthySourcesAllRejected_StaysPermanent(t *testing.T) {
	quiet := findOnlySource{name: "quiet"}

	ctx, err := searchThenDownloadRejectingAll(t, quiet)

	if ports.IsSourceUnavailable(err) || willRetry(ctx, err) {
		t.Fatalf("err = %v, want a permanent failure", err)
	}
}

type urlIdentifier struct {
	fetcher *gatedFetcher
	matches map[string]ports.RecordingMatch
}

func (i *urlIdentifier) Identify(_ context.Context, path string, _ float64) (ports.RecordingMatch, error) {
	for url, match := range i.matches {
		if i.fetcher.dirOf(url) == filepath.Dir(path) {
			return match, nil
		}
	}
	return ports.RecordingMatch{}, nil
}

func (i *urlIdentifier) AcoustIDsFor(context.Context, string) ([]string, error) { return nil, nil }

func hardMatch() ports.RecordingMatch {
	return ports.RecordingMatch{
		AcoustID: "ac-a", MBIDs: []string{"mb-master"}, Score: 0.97,
		Results: linkedResults(ports.LinkedRecording{MBID: "mb-master", Title: "Sunglasses at Night"}),
	}
}

func heldTrackContext(ranked ...ports.AudioCandidate) *AcquisitionContext {
	return &AcquisitionContext{
		Track:    TrackRef{Title: "Sunglasses at Night", Artist: "Corey Hart", Duration: 236},
		Identity: ports.RecordingIdentity{MBID: "mb-master"},
		Ranked:   ranked,
	}
}

func TestDownloadStep_WalksPastUnknownFingerprintToALaterHardMatch(t *testing.T) {
	for _, width := range []int{1, 2} {
		t.Run(fmt.Sprintf("width %d", width), func(t *testing.T) {
			fetcher := newGatedFetcher(nil)
			identifier := &urlIdentifier{fetcher: fetcher, matches: map[string]ports.RecordingMatch{"hard": hardMatch()}}
			ac := heldTrackContext(
				ports.AudioCandidate{URL: "unknown", Channel: "Corey Hart - Topic", Duration: 236},
				ports.AudioCandidate{URL: "hard", Channel: "someone", Duration: 236},
			)
			step := NewDownloadStep(fetcher, WithDownloadIdentifier(identifier), WithVerifyWidth(width))

			if _, err := step.Execute(context.Background(), ac, afterSelect{}); err != nil {
				t.Fatalf("Execute: %v", err)
			}
			defer os.RemoveAll(filepath.Dir(ac.TempPath))

			if ac.Selected == nil || ac.Selected.URL != "hard" {
				t.Fatalf("selected = %+v, want the hard match", ac.Selected)
			}
			if got := ac.Provenance(); got != domain.ProvenanceVerified {
				t.Errorf("provenance = %q, want verified", got)
			}
			if _, err := os.Stat(fetcher.dirOf("unknown")); !os.IsNotExist(err) {
				t.Errorf("the held unknown candidate's temp dir should be removed, stat err = %v", err)
			}
		})
	}
}

func TestDownloadStep_CleanHoldBeatsAHigherRankedFallbackHold(t *testing.T) {
	fetcher := newGatedFetcher(nil)
	identifier := &urlIdentifier{fetcher: fetcher, matches: map[string]ports.RecordingMatch{"edit": hardMatch()}}
	ac := heldTrackContext(
		ports.AudioCandidate{URL: "edit", Title: "Sunglasses at Night (Radio Edit)", Channel: "Corey Hart - Topic", Duration: 236},
		ports.AudioCandidate{URL: "clean", Title: "Sunglasses at Night", Channel: "Corey Hart - Topic", Duration: 236},
	)
	step := NewDownloadStep(fetcher, WithDownloadIdentifier(identifier))

	if _, err := step.Execute(context.Background(), ac, afterSelect{}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(ac.TempPath))

	if ac.Selected == nil || ac.Selected.URL != "clean" {
		t.Fatalf("selected = %+v, want the clean hold", ac.Selected)
	}
	if !ac.BestEffort || ac.Provenance() != domain.ProvenanceBestEffort {
		t.Errorf("a held selection must be best_effort, got BestEffort=%v provenance=%q", ac.BestEffort, ac.Provenance())
	}
	if _, err := os.Stat(fetcher.dirOf("edit")); !os.IsNotExist(err) {
		t.Errorf("the displaced fallback hold's temp dir should be removed, stat err = %v", err)
	}
}

func TestDownloadStep_HeldCandidateBelowTheFloorFailsAsNoConfidentMatch(t *testing.T) {
	fetcher := newGatedFetcher(nil)
	ac := heldTrackContext(ports.AudioCandidate{URL: "reupload", Channel: "chill uploads", Duration: 256})
	step := NewDownloadStep(fetcher, WithDownloadIdentifier(&urlIdentifier{fetcher: fetcher}))

	_, err := step.Execute(context.Background(), ac, afterSelect{})

	if !errors.Is(err, ErrNoConfidentMatch) {
		t.Fatalf("err = %v, want ErrNoConfidentMatch", err)
	}
	if _, statErr := os.Stat(fetcher.dirOf("reupload")); !os.IsNotExist(statErr) {
		t.Errorf("the refused hold's temp dir should be removed, stat err = %v", statErr)
	}
}

func TestDownloadStep_FallbackOfTheWrongLengthIsNeverStored(t *testing.T) {
	fetcher := newGatedFetcher(nil)
	ac := heldTrackContext(ports.AudioCandidate{
		URL: "edit", Title: "Sunglasses at Night (Radio Edit)", Channel: "Corey Hart - Topic", Duration: 236,
	})
	prober := scriptedProber{duration: 245}
	step := NewDownloadStep(fetcher, WithDownloadProber(prober))

	_, err := step.Execute(context.Background(), ac, afterSelect{})

	if !errors.Is(err, ErrNoConfidentMatch) {
		t.Fatalf("err = %v, want ErrNoConfidentMatch", err)
	}
}

func TestDownloadStep_EveryCandidateImplausibleFailsAsNoConfidentMatch(t *testing.T) {
	ac := heldTrackContext(ports.AudioCandidate{URL: "far", Duration: 100})
	step := NewDownloadStep(&fileWritingSearcher{writeFile: true})

	_, err := step.Execute(context.Background(), ac, afterSelect{})

	if !errors.Is(err, ErrNoConfidentMatch) {
		t.Fatalf("err = %v, want ErrNoConfidentMatch", err)
	}
}
