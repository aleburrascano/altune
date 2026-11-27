package service_test

import (
	"altune/go-api/internal/download/downloadtest"
	"altune/go-api/internal/download/internal/adapters/inmemoryledger"
	acqdomain "altune/go-api/internal/download/internal/core/domain"
	"altune/go-api/internal/download/internal/core/isrcwriteback"
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/download/internal/core/rejectedcandidates"
	length "altune/go-api/internal/download/internal/core/tracklength"
	"altune/go-api/internal/download/service"
	"altune/go-api/internal/download/service/musicbrainzmatch"
	"altune/go-api/internal/download/sourcekey"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/downloadfailure"
	"altune/go-api/internal/shared/downloadprovenance"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/random"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

type fileWritingSearcher struct {
	mu        sync.Mutex
	writeFile bool
	err       error
	gotURL    string
	gotDir    string
	calls     int
}

func (s *fileWritingSearcher) Search(_ context.Context, _ string) ([]ports.AudioCandidate, error) {
	return nil, nil
}

func (s *fileWritingSearcher) Name() acqdomain.SourceName { return "filewriting" }

func (s *fileWritingSearcher) Find(_ context.Context, _ ports.FindRequest) ([]ports.AudioCandidate, error) {
	return nil, nil
}

func (s *fileWritingSearcher) Fetch(ctx context.Context, c ports.AudioCandidate, outDir string) (ports.FetchedAudio, error) {
	return fetchedFrom(s.Download(ctx, c.URL, outDir))
}

func (s *fileWritingSearcher) Download(_ context.Context, url, outDir string) (string, error) {
	s.mu.Lock()
	s.calls++
	s.gotURL = url
	s.gotDir = outDir
	s.mu.Unlock()
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
	step := newTestDownloadStep(searcher)
	ac := &service.AcquisitionContext{Ranked: []ports.AudioCandidate{{URL: "https://example.com/x"}}}

	if _, err := step.Execute(context.Background(), ac, service.AfterSelect{}); err != nil {
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
	step := newTestDownloadStep(&fileWritingSearcher{})
	if _, err := step.Execute(context.Background(), &service.AcquisitionContext{}, service.AfterSelect{}); err == nil {
		t.Fatal("expected error when no candidate is selected")
	}
}

func TestConstructors_RefuseAMissingTempWorkspace(t *testing.T) {
	builders := map[string]func() error{
		"NewDownloadStep": func() error {
			_, err := service.NewDownloadStep(previewingOrPlain(&fileWritingSearcher{}), nil, ports.NoopAudioIdentifier(), ports.NoopRecordingLookup(), downloadtest.SilentAudioProber(), ports.NoopVerifySkipRecorder(), service.Unlimited(), time.Now)
			return err
		},
		"NewStoreStep": func() error {
			_, err := service.NewStoreStep(nil, nil, downloadtest.SilentAudioProber(), service.MustDirectDeleteOrphans(discardingDeleter{}), downloadtest.SilentAudioRefLookup(), clock.Sleep, random.Real)
			return err
		},
		"NewAcquireTrackAudioService": func() error {
			_, err := service.NewAcquireTrackAudioService(nil, nil, nil, nil, noopVerification(), noopStages())
			return err
		},
	}
	for name, build := range builders {
		t.Run(name, func(t *testing.T) {
			err := build()

			if !errors.Is(err, service.ErrMissingCollaborator) || !strings.Contains(err.Error(), name) || !strings.Contains(err.Error(), "workspace") {
				t.Errorf("%s error = %v, want ErrMissingCollaborator naming the constructor and the workspace", name, err)
			}
		})
	}
}

func TestConstructors_RefuseAMissingLedgerSourcesFetcherOrAudioStore(t *testing.T) {
	cases := map[string]func() error{
		"ledger": func() error {
			_, err := service.NewAcquireTrackAudioService(nil, service.NewSourceRegistry(), newFakeAudioStore(), osWorkspace{}, noopVerification(), noopStages())
			return err
		},
		"source registry": func() error {
			_, err := service.NewAcquireTrackAudioService(inmemoryledger.NewLedger(), nil, newFakeAudioStore(), osWorkspace{}, noopVerification(), noopStages())
			return err
		},
		"audio store": func() error {
			_, err := service.NewAcquireTrackAudioService(inmemoryledger.NewLedger(), service.NewSourceRegistry(), nil, osWorkspace{}, noopVerification(), noopStages())
			return err
		},
		"fetcher": func() error {
			_, err := service.NewDownloadStep(nil, osWorkspace{}, ports.NoopAudioIdentifier(), ports.NoopRecordingLookup(), downloadtest.SilentAudioProber(), ports.NoopVerifySkipRecorder(), service.Unlimited(), time.Now)
			return err
		},
		"sleeper": func() error {
			_, err := service.NewStoreStep(newFakeAudioStore(), osWorkspace{}, downloadtest.SilentAudioProber(), service.MustDirectDeleteOrphans(discardingDeleter{}), downloadtest.SilentAudioRefLookup(), nil, random.Real)
			return err
		},
		"jitter source": func() error {
			_, err := service.NewStoreStep(newFakeAudioStore(), osWorkspace{}, downloadtest.SilentAudioProber(), service.MustDirectDeleteOrphans(discardingDeleter{}), downloadtest.SilentAudioRefLookup(), clock.Sleep, nil)
			return err
		},
		"clock": func() error {
			_, err := service.NewDownloadStep(previewingOrPlain(&fileWritingSearcher{}), osWorkspace{}, ports.NoopAudioIdentifier(), ports.NoopRecordingLookup(), downloadtest.SilentAudioProber(), ports.NoopVerifySkipRecorder(), service.Unlimited(), nil)
			return err
		},
		"audio store step audio store": func() error {
			_, err := service.NewStoreStep(nil, osWorkspace{}, downloadtest.SilentAudioProber(), service.MustDirectDeleteOrphans(discardingDeleter{}), downloadtest.SilentAudioRefLookup(), clock.Sleep, random.Real)
			return err
		},
	}
	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			err := build()

			if !errors.Is(err, service.ErrMissingCollaborator) || !strings.Contains(err.Error(), strings.TrimSuffix(name, " step audio store")) {
				t.Errorf("error = %v, want ErrMissingCollaborator naming %q", err, name)
			}
		})
	}
}

func TestConstructors_RefuseAMissingVerificationPort(t *testing.T) {
	cases := map[string]func(*service.VerificationPorts){
		"Identifier":      func(v *service.VerificationPorts) { v.Identifier = nil },
		"RecordingLookup": func(v *service.VerificationPorts) { v.RecordingLookup = nil },
		"Rerecordings":    func(v *service.VerificationPorts) { v.Rerecordings = nil },
		"Previews":        func(v *service.VerificationPorts) { v.Previews = nil },
		"Recordings":      func(v *service.VerificationPorts) { v.Recordings = nil },
	}
	for field, drop := range cases {
		t.Run(field, func(t *testing.T) {
			wiring := noopVerification()
			drop(&wiring)

			_, err := service.NewAcquireTrackAudioService(nil, nil, nil, osWorkspace{}, wiring, noopStages())

			if !errors.Is(err, service.ErrMissingCollaborator) || !strings.Contains(err.Error(), field) {
				t.Errorf("error = %v, want ErrMissingCollaborator naming %s", err, field)
			}
		})
	}
}

func TestConstructors_RefuseAMissingStagePort(t *testing.T) {
	cases := map[string]func(*service.StagePorts){
		"Prober":        func(p *service.StagePorts) { p.Prober = nil },
		"Tagger":        func(p *service.StagePorts) { p.Tagger = nil },
		"Rejections":    func(p *service.StagePorts) { p.Rejections = nil },
		"StoredAudio":   func(p *service.StagePorts) { p.StoredAudio = nil },
		"Orphans":       func(p *service.StagePorts) { p.Orphans = nil },
		"Skips":         func(p *service.StagePorts) { p.Skips = nil },
		"Events":        func(p *service.StagePorts) { p.Events = nil },
		"DownloadLimit": func(p *service.StagePorts) { p.DownloadLimit = nil },
		"Observer":      func(p *service.StagePorts) { p.Observer = nil },
		"Sleep":         func(p *service.StagePorts) { p.Sleep = nil },
		"Now":           func(p *service.StagePorts) { p.Now = nil },
		"Unit":          func(p *service.StagePorts) { p.Unit = nil },
	}
	for field, drop := range cases {
		t.Run(field, func(t *testing.T) {
			stages := noopStages()
			drop(&stages)

			_, err := service.NewAcquireTrackAudioService(nil, nil, nil, osWorkspace{}, noopVerification(), stages)

			if !errors.Is(err, service.ErrMissingCollaborator) || !strings.Contains(err.Error(), field) {
				t.Errorf("error = %v, want ErrMissingCollaborator naming %s", err, field)
			}
		})
	}
}

func TestNewTagStep_RefusesAMissingTagger(t *testing.T) {
	_, err := service.NewTagStep(nil)

	if !errors.Is(err, service.ErrMissingCollaborator) || !strings.Contains(err.Error(), "tagger") {
		t.Errorf("error = %v, want ErrMissingCollaborator naming the tagger", err)
	}
}

func TestNewStoreStep_RefusesAMissingCollaborator(t *testing.T) {
	_, err := service.NewStoreStep(nil, osWorkspace{}, nil, nil, nil, nil, nil)

	for _, name := range []string{"prober", "orphan recorder", "audio ref lookup", "sleeper", "jitter source"} {
		if !errors.Is(err, service.ErrMissingCollaborator) || !strings.Contains(err.Error(), name) {
			t.Errorf("error = %v, want ErrMissingCollaborator naming the %s", err, name)
		}
	}
}

func TestDirectDeleteOrphans_RefusesAMissingAudioStore(t *testing.T) {
	_, err := service.DirectDeleteOrphans(nil)

	if !errors.Is(err, service.ErrMissingCollaborator) || !strings.Contains(err.Error(), "audio store") {
		t.Errorf("error = %v, want ErrMissingCollaborator naming the audio store", err)
	}
}

func TestCoreSteps_RefuseAMissingCollaborator(t *testing.T) {
	_, err := service.CoreSteps(service.NewSourceRegistry(), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, time.Now, osWorkspace{}, nil, nil)

	for _, name := range []string{"identifier", "tagger", "prober", "download limit", "orphan recorder"} {
		if !errors.Is(err, service.ErrMissingCollaborator) || !strings.Contains(err.Error(), name) {
			t.Errorf("error = %v, want ErrMissingCollaborator naming the %s", err, name)
		}
	}
}

func TestNewDownloadStep_RefusesAMissingCollaborator(t *testing.T) {
	builders := map[string]func() error{
		"identifier": func() error {
			_, err := service.NewDownloadStep(nil, osWorkspace{}, nil, ports.NoopRecordingLookup(), downloadtest.SilentAudioProber(), ports.NoopVerifySkipRecorder(), service.Unlimited(), time.Now)
			return err
		},
		"recording lookup": func() error {
			_, err := service.NewDownloadStep(nil, osWorkspace{}, ports.NoopAudioIdentifier(), nil, downloadtest.SilentAudioProber(), ports.NoopVerifySkipRecorder(), service.Unlimited(), time.Now)
			return err
		},
		"prober": func() error {
			_, err := service.NewDownloadStep(nil, osWorkspace{}, ports.NoopAudioIdentifier(), ports.NoopRecordingLookup(), nil, ports.NoopVerifySkipRecorder(), service.Unlimited(), time.Now)
			return err
		},
		"skip recorder": func() error {
			_, err := service.NewDownloadStep(nil, osWorkspace{}, ports.NoopAudioIdentifier(), ports.NoopRecordingLookup(), downloadtest.SilentAudioProber(), nil, service.Unlimited(), time.Now)
			return err
		},
		"download limit": func() error {
			_, err := service.NewDownloadStep(nil, osWorkspace{}, ports.NoopAudioIdentifier(), ports.NoopRecordingLookup(), downloadtest.SilentAudioProber(), ports.NoopVerifySkipRecorder(), nil, time.Now)
			return err
		},
	}
	for port, build := range builders {
		t.Run(port, func(t *testing.T) {
			err := build()

			if !errors.Is(err, service.ErrMissingCollaborator) || !strings.Contains(err.Error(), port) {
				t.Errorf("error = %v, want ErrMissingCollaborator naming the %s", err, port)
			}
		})
	}
}

func TestDownloadStep_Execute_DownloadError_CleansTempDir(t *testing.T) {
	searcher := &fileWritingSearcher{err: errors.New("boom")}
	step := newTestDownloadStep(searcher)
	ac := &service.AcquisitionContext{Ranked: []ports.AudioCandidate{{URL: "https://example.com/x"}}}

	if _, err := step.Execute(context.Background(), ac, service.AfterSelect{}); err == nil {
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

	step := newTestDownloadStep(&fileWritingSearcher{})
	if err := step.Rollback(context.Background(), &service.AcquisitionContext{TempPath: file}); err != nil {
		t.Fatalf("Rollback error: %v", err)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Error("temp file should be removed after rollback")
	}
}

type cancellingFetcher struct {
	cancel func()
	err    error
	calls  atomic.Int32
}

func (f *cancellingFetcher) Fetch(_ context.Context, _ ports.AudioCandidate, _ string) (ports.FetchedAudio, error) {
	f.calls.Add(1)
	if f.cancel != nil {
		f.cancel()
	}
	return fetchedFrom("", f.err)
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
			wantMax: service.ProdVerifyWidth,
		},
		{
			name:  "cancel mid-loop, adapter wraps ctx cause",
			fetch: context.Canceled,
			newCtx: func() (context.Context, context.CancelFunc) {
				return context.WithCancel(context.Background())
			},
			wantMax: service.ProdVerifyWidth,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := tt.newCtx()
			defer cancel()
			fetcher := &cancellingFetcher{cancel: cancel, err: tt.fetch}
			step := newTestDownloadStep(fetcher)
			ac := &service.AcquisitionContext{Ranked: []ports.AudioCandidate{
				{URL: "https://example.com/a"},
				{URL: "https://example.com/b"},
				{URL: "https://example.com/c"},
			}}

			_, err := step.Execute(ctx, ac, service.AfterSelect{})
			if err == nil {
				t.Fatal("expected an error from a cancelled download")
			}
			if got := service.FailureCodeOf(&service.StepError{Step: "download", Err: err}); got != string(downloadfailure.FailureAcquisitionCancelled) {
				t.Errorf("failureReason = %q, want %q", got, downloadfailure.FailureAcquisitionCancelled)
			}
			if got := int(fetcher.calls.Load()); got > tt.wantMax {
				t.Errorf("attempted %d candidates after cancellation, want <= %d (the one window already in flight)", got, tt.wantMax)
			}
		})
	}
}

func TestDownloadStep_CancelledOnLastCandidate_ReportsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fetcher := &cancellingFetcher{cancel: cancel, err: errors.New("connection reset")}
	step := newTestDownloadStep(fetcher)
	ac := &service.AcquisitionContext{Ranked: []ports.AudioCandidate{{URL: "https://example.com/only"}}}

	_, err := step.Execute(ctx, ac, service.AfterSelect{})
	if err == nil {
		t.Fatal("expected an error from a cancelled download")
	}
	if got := service.FailureCodeOf(&service.StepError{Step: "download", Err: err}); got != string(downloadfailure.FailureAcquisitionCancelled) {
		t.Errorf("failureReason = %q, want %q", got, downloadfailure.FailureAcquisitionCancelled)
	}
}

func TestDownloadStep_DeadlineExpiredBeforeStart_ReportsCancellation(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	fetcher := &cancellingFetcher{err: errors.New("unused")}
	step := newTestDownloadStep(fetcher)
	ac := &service.AcquisitionContext{Ranked: []ports.AudioCandidate{{URL: "https://example.com/a"}}}

	_, err := step.Execute(ctx, ac, service.AfterSelect{})
	if err == nil {
		t.Fatal("expected an error from an expired deadline")
	}
	if got := service.FailureCodeOf(&service.StepError{Step: "download", Err: err}); got != string(downloadfailure.FailureAcquisitionCancelled) {
		t.Errorf("failureReason = %q, want %q", got, downloadfailure.FailureAcquisitionCancelled)
	}
	if fetcher.calls.Load() != 0 {
		t.Errorf("attempted %d candidates with an expired deadline, want 0", fetcher.calls.Load())
	}
}

func TestDownloadStep_GenuineFailure_KeepsDownloadReasonAndTriesAll(t *testing.T) {
	fetcher := &cancellingFetcher{err: errors.New("yt-dlp: exit 1")}
	step := newTestDownloadStep(fetcher)
	ac := &service.AcquisitionContext{Ranked: []ports.AudioCandidate{
		{URL: "https://example.com/a"},
		{URL: "https://example.com/b"},
	}}

	_, err := step.Execute(context.Background(), ac, service.AfterSelect{})
	if err == nil {
		t.Fatal("expected a download error")
	}
	if got := service.FailureCodeOf(&service.StepError{Step: "download", Err: err}); got != string(downloadfailure.FailureDownloadFailed) {
		t.Errorf("failureReason = %q, want %q", got, downloadfailure.FailureDownloadFailed)
	}
	if fetcher.calls.Load() != 2 {
		t.Errorf("attempted %d candidates, want 2 (whole list under a live context)", fetcher.calls.Load())
	}
}

func TestDownloadStep_ImplausibleSearchDuration_IsSkippedBeforeFetch(t *testing.T) {
	const mix = "https://example.com/three-hour-mix"
	searcher := &fileWritingSearcher{writeFile: true}
	step := newTestDownloadStep(searcher)
	ac := &service.AcquisitionContext{
		Track: service.TrackRef{Title: "X", Artist: "Y", CatalogDuration: 200},
		Ranked: []ports.AudioCandidate{
			{URL: mix, Duration: 10800},
			{URL: "https://example.com/track", Duration: 201},
		},
	}

	if _, err := step.Execute(context.Background(), ac, service.AfterSelect{}); err != nil {
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
	if got := ac.Rejections[0]; got.URL != mix || got.Stage != rejectedcandidates.Duration {
		t.Errorf("rejection = %+v, want %q at stage %q", got, mix, rejectedcandidates.Duration)
	}
}

func TestDownloadStep_CandidateSearchDurationCannotDisqualify_IsStillFetched(t *testing.T) {
	for _, tt := range []struct {
		name      string
		track     service.TrackRef
		candidate ports.AudioCandidate
	}{
		{
			name:      "search reported no duration",
			track:     service.TrackRef{CatalogDuration: 200},
			candidate: ports.AudioCandidate{URL: "https://example.com/x"},
		},
		{
			name:      "the track has no saved duration to compare against",
			track:     service.TrackRef{},
			candidate: ports.AudioCandidate{URL: "https://example.com/x", Duration: 10800},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			searcher := &fileWritingSearcher{writeFile: true}
			step := newTestDownloadStep(searcher)
			ac := &service.AcquisitionContext{Track: tt.track, Ranked: []ports.AudioCandidate{tt.candidate}}

			if _, err := step.Execute(context.Background(), ac, service.AfterSelect{}); err != nil {
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

func TestDownloadStep_AResolvedCandidateFarFromTheTargetLength_IsSkippedBeforeFetch(t *testing.T) {
	const mix = "https://example.com/resolved-mix"
	searcher := &fileWritingSearcher{writeFile: true}
	step := newTestDownloadStep(searcher)
	ac := &service.AcquisitionContext{
		Track: service.TrackRef{Title: "X", Artist: "Y", CatalogDuration: 200},
		Ranked: []ports.AudioCandidate{
			{URL: mix, Duration: 10800, Kind: ports.KindArtTrack},
			{URL: "https://example.com/track", Duration: 201},
		},
	}

	if _, err := step.Execute(context.Background(), ac, service.AfterSelect{}); err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(ac.TempPath))

	if searcher.calls != 1 || searcher.gotURL != "https://example.com/track" {
		t.Errorf("fetched %d candidates, last %q, want only the track: the resolved 10800 s candidate must never be downloaded", searcher.calls, searcher.gotURL)
	}
	if len(ac.Rejections) != 1 {
		t.Fatalf("rejections = %+v, want exactly one, for the resolved candidate", ac.Rejections)
	}
	if got := ac.Rejections[0]; got.URL != mix || got.Stage != rejectedcandidates.Duration {
		t.Errorf("rejection = %+v, want %q at stage %q", got, mix, rejectedcandidates.Duration)
	}
}

func TestDownloadStep_SkippedCandidatesDoNotSpendTheAttemptBudget(t *testing.T) {
	ranked := make([]ports.AudioCandidate, 0, service.MaxDownloadAttempts+1)
	for i := 0; i < service.MaxDownloadAttempts; i++ {
		ranked = append(ranked, ports.AudioCandidate{URL: "https://example.com/mix", Duration: 10800})
	}
	ranked = append(ranked, ports.AudioCandidate{URL: "https://example.com/track", Duration: 200})

	searcher := &fileWritingSearcher{writeFile: true}
	step := newTestDownloadStep(searcher)
	ac := &service.AcquisitionContext{Track: service.TrackRef{CatalogDuration: 200}, Ranked: ranked}

	if _, err := step.Execute(context.Background(), ac, service.AfterSelect{}); err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(ac.TempPath))

	if searcher.gotURL != "https://example.com/track" {
		t.Errorf("fetched %q, want the candidate past %d skipped ones", searcher.gotURL, service.MaxDownloadAttempts)
	}
}

type sequenceFetcher struct {
	errs  []error
	calls int
}

func (f *sequenceFetcher) Fetch(_ context.Context, _ ports.AudioCandidate, _ string) (ports.FetchedAudio, error) {
	err := f.errs[f.calls]
	f.calls++
	return fetchedFrom("", err)
}

func TestDownloadStep_WidthOne_EarlierUnavailableSurvivesADifferentLastFailure(t *testing.T) {
	fetcher := &sequenceFetcher{errs: []error{
		&acqdomain.SourceUnavailableError{Source: "ytdlp", Err: errors.New("HTTP Error 429")},
		errors.New("exit 1"),
	}}
	ac := &service.AcquisitionContext{Ranked: []ports.AudioCandidate{{URL: "u1"}, {URL: "u2"}}}

	_, err := newTestDownloadStep(fetcher, service.WithVerifyWidth(1)).Execute(context.Background(), ac, service.AfterSelect{})

	if code := failureCodeOf(&service.StepError{Step: service.StepNameDownload, Err: err}); code != downloadfailure.FailureSourceUnavailable {
		t.Fatalf("failure code = %s, want %s", code, downloadfailure.FailureSourceUnavailable)
	}
}

type queueProber struct {
	mu         sync.Mutex
	durations  []float64
	calls      int
	decodeErrs []error
	decCalls   int
}

func (p *queueProber) ProbeDuration(_ context.Context, _ string) (float64, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	d := p.durations[p.calls]
	p.calls++
	return d, nil
}

func (p *queueProber) ValidateDecodable(_ context.Context, _ string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	var err error
	if p.decCalls < len(p.decodeErrs) {
		err = p.decodeErrs[p.decCalls]
	}
	p.decCalls++
	return err
}

func TestDownloadStep_WidthOne_ProbeQueueVerifiesAndFallsBack(t *testing.T) {
	searcher := &fileWritingSearcher{writeFile: true}
	prober := &queueProber{durations: []float64{840, 227}}
	step := newTestDownloadStep(searcher, withTestProber(prober), service.WithVerifyWidth(1))

	ac := &service.AcquisitionContext{
		Track: service.TrackRef{Title: "How Sweet", Artist: "NewJeans", CatalogDuration: 226},
		Ranked: []ports.AudioCandidate{
			{URL: "https://youtube.com/watch?v=bloated", Duration: 0},
			{URL: "https://youtube.com/watch?v=correct", Duration: 227},
		},
	}

	if _, err := step.Execute(context.Background(), ac, service.AfterSelect{}); err != nil {
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
	step := newTestDownloadStep(searcher, withTestProber(prober))

	ac := &service.AcquisitionContext{
		Track:  service.TrackRef{Title: "How Sweet", Artist: "NewJeans", CatalogDuration: 226},
		Ranked: []ports.AudioCandidate{{URL: "https://youtube.com/watch?v=bloated", Duration: 840}},
	}

	if _, err := step.Execute(context.Background(), ac, service.AfterSelect{}); err == nil {
		t.Fatal("expected an error when no candidate matches the expected duration")
	}
	if ac.TempPath != "" {
		t.Errorf("TempPath must stay empty when all candidates are rejected, got %q", ac.TempPath)
	}
}

func TestDownloadStep_AnOfficialTrackIsCheckedAgainstItsOwnLength(t *testing.T) {
	cases := map[string]struct {
		target      float64
		fileSeconds float64
		accepted    bool
	}{
		"file matches the official length and the target":               {target: 168, fileSeconds: 168, accepted: true},
		"file is shorter than the official length":                      {target: 168, fileSeconds: 122, accepted: false},
		"file matches the official length but is off the target length": {target: 122, fileSeconds: 168, accepted: false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			step := newTestDownloadStep(&fileWritingSearcher{writeFile: true},
				withTestProber(&queueProber{durations: []float64{tc.fileSeconds}}))
			ac := &service.AcquisitionContext{
				Track: service.TrackRef{Title: "Demigod", Artist: "SLAYR", CatalogDuration: tc.target},
				Ranked: []ports.AudioCandidate{
					{URL: "https://music.youtube.com/watch?v=official", Duration: 168, Kind: ports.KindArtTrack},
				},
			}

			_, err := step.Execute(context.Background(), ac, service.AfterSelect{})
			if ac.TempPath != "" {
				defer os.RemoveAll(filepath.Dir(ac.TempPath))
			}
			if accepted := err == nil && ac.TempPath != ""; accepted != tc.accepted {
				t.Fatalf("accepted = %v (err %v), want %v", accepted, err, tc.accepted)
			}
		})
	}
}

func TestDownloadStep_AResolvedTrackMustMatchTheTargetLength(t *testing.T) {
	cases := map[string]struct {
		byline      float64
		fileSeconds float64
		accepted    bool
	}{
		"file matches its byline but is 30 s off the catalog length": {byline: 294, fileSeconds: 294, accepted: false},
		"file matches its byline and the catalog length":             {byline: 264, fileSeconds: 264, accepted: true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			step := newTestDownloadStep(&fileWritingSearcher{writeFile: true},
				withTestProber(&queueProber{durations: []float64{tc.fileSeconds}}))
			ac := &service.AcquisitionContext{
				Track: service.TrackRef{Title: "Human", Artist: "The Human League", CatalogDuration: 264},
				Ranked: []ports.AudioCandidate{
					{URL: "https://music.youtube.com/watch?v=art", Duration: tc.byline, Kind: ports.KindArtTrack},
				},
			}

			_, err := step.Execute(context.Background(), ac, service.AfterSelect{})
			if ac.TempPath != "" {
				defer os.RemoveAll(filepath.Dir(ac.TempPath))
			}
			if accepted := err == nil && ac.TempPath != ""; accepted != tc.accepted {
				t.Fatalf("accepted = %v (err %v), want %v", accepted, err, tc.accepted)
			}
		})
	}
}

func TestDownloadStep_AProbedFileOffTheTargetLengthIsRejectedAfterDownload(t *testing.T) {
	art := ports.AudioCandidate{URL: "https://music.youtube.com/watch?v=art", Duration: 275, Kind: ports.KindArtTrack}
	searcher := &fileWritingSearcher{writeFile: true}
	step := newTestDownloadStep(searcher, withTestProber(&queueProber{durations: []float64{283}}))
	ac := &service.AcquisitionContext{
		Track:  service.TrackRef{Title: "Human", Artist: "The Human League", CatalogDuration: 264},
		Ranked: []ports.AudioCandidate{art},
	}

	_, err := step.Execute(context.Background(), ac, service.AfterSelect{})
	if ac.TempPath != "" {
		defer os.RemoveAll(filepath.Dir(ac.TempPath))
	}

	if err == nil || ac.TempPath != "" {
		t.Fatalf("err = %v, TempPath = %q, want the file rejected", err, ac.TempPath)
	}
	if searcher.calls != 1 {
		t.Errorf("fetched %d candidates, want 1: the byline is within tolerance of the target, so only the probed file can fail", searcher.calls)
	}
	if len(ac.Rejections) != 1 || ac.Rejections[0].Stage != rejectedcandidates.Duration || ac.Rejections[0].Reason != "duration 283s vs expected 264s" {
		t.Errorf("rejections = %+v, want one duration rejection naming the 264 s target", ac.Rejections)
	}
}

func TestDownloadStep_NoExpectedDuration_SkipsVerification(t *testing.T) {
	searcher := &fileWritingSearcher{writeFile: true}
	prober := &queueProber{durations: []float64{840}}
	step := newTestDownloadStep(searcher, withTestProber(prober))

	ac := &service.AcquisitionContext{
		Track:  service.TrackRef{Title: "Unknown", Artist: "Artist", CatalogDuration: 0},
		Ranked: []ports.AudioCandidate{{URL: "https://youtube.com/watch?v=whatever", Duration: 840}},
	}

	if _, err := step.Execute(context.Background(), ac, service.AfterSelect{}); err != nil {
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

func TestDownloadStep_WidthOne_DecodeQueueRejectsUndecodableAudio(t *testing.T) {
	searcher := &fileWritingSearcher{writeFile: true}
	prober := &queueProber{
		durations:  []float64{226, 226},
		decodeErrs: []error{errors.New("audio stream failed to decode"), nil},
	}
	step := newTestDownloadStep(searcher, withTestProber(prober), service.WithVerifyWidth(1))

	ac := &service.AcquisitionContext{
		Track: service.TrackRef{Title: "X", Artist: "Y", CatalogDuration: 226},
		Ranked: []ports.AudioCandidate{
			{URL: "https://youtube.com/watch?v=corruptAAAA", Duration: 226},
			{URL: "https://youtube.com/watch?v=goodAAAAAAA", Duration: 226},
		},
	}

	if _, err := step.Execute(context.Background(), ac, service.AfterSelect{}); err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(ac.TempPath))

	if ac.Selected == nil || ac.Selected.URL != "https://youtube.com/watch?v=goodAAAAAAA" {
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

func rejectionBasisOf(ac *service.AcquisitionContext) ports.RejectionBasis {
	return ports.RejectionBasis{ExpectedSeconds: ac.ExpectedLength(), ReferenceMBID: ac.Identity.MBID}
}

func lengthsOf(ac *service.AcquisitionContext) length.Policy {
	if ac.Identity.ReferenceDoubted {
		return length.Doubted(ac.Track.CatalogDuration)
	}
	return length.New(ac.Track.CatalogDuration, ac.Identity.Duration)
}

func provenISRCOf(ac *service.AcquisitionContext) (string, isrcwriteback.Source) {
	return isrcwriteback.Prove(isrcwriteback.Facts{
		CatalogISRC: ac.Track.ISRC,
		CatalogMBID: ac.Track.CatalogMBID,
		Identity:    ac.Identity,
		HardVerdict: ac.Verdict.Kind == service.VerdictHard,
		Surviving:   ac.Verdict.Surviving,
	})
}

func TestDownloadStep_DecodeCancelled_RecordsNoLastingRejection(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	step := newTestDownloadStep(&fileWritingSearcher{writeFile: true}, withTestProber(cancellingProber{cancel: cancel}))
	ac := &service.AcquisitionContext{
		Track:  service.TrackRef{ID: "t1", Title: "X", Artist: "Y", CatalogDuration: 226},
		Ranked: []ports.AudioCandidate{{URL: "https://youtube.com/watch?v=abc", Duration: 226}},
	}

	_, err := step.Execute(ctx, ac, service.AfterSelect{})

	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("Execute error = %v, want one wrapping context.Canceled", err)
	}
	if recs := rejectedcandidates.Lasting(ac.Rejections, ac.Track.ID, rejectionBasisOf(ac)); len(recs) != 0 {
		t.Errorf("lasting rejection records = %+v, want none for a cancelled decode", recs)
	}
}

func TestDownloadStep_DecodeFailureWithLiveContext_RecordsUndecodable(t *testing.T) {
	prober := &queueProber{
		durations:  []float64{226},
		decodeErrs: []error{errors.New("audio stream failed to decode")},
	}
	step := newTestDownloadStep(&fileWritingSearcher{writeFile: true}, withTestProber(prober))
	ac := &service.AcquisitionContext{
		Track:  service.TrackRef{ID: "t1", Title: "X", Artist: "Y", CatalogDuration: 226},
		Ranked: []ports.AudioCandidate{{URL: "https://youtube.com/watch?v=abcabcabcab", Duration: 226}},
	}

	if _, err := step.Execute(context.Background(), ac, service.AfterSelect{}); err == nil {
		t.Fatal("expected an error when the only candidate is undecodable")
	}
	recs := rejectedcandidates.Lasting(ac.Rejections, ac.Track.ID, rejectionBasisOf(ac))
	if len(recs) != 1 || recs[0].Reason != string(rejectedcandidates.Undecodable) {
		t.Errorf("lasting rejection records = %+v, want one undecodable", recs)
	}
}

func TestDownloadStep_AllUndecodable_Errors(t *testing.T) {
	searcher := &fileWritingSearcher{writeFile: true}
	prober := &queueProber{
		durations:  []float64{226},
		decodeErrs: []error{errors.New("audio stream failed to decode")},
	}
	step := newTestDownloadStep(searcher, withTestProber(prober))

	ac := &service.AcquisitionContext{
		Track:  service.TrackRef{Title: "X", Artist: "Y", CatalogDuration: 226},
		Ranked: []ports.AudioCandidate{{URL: "https://youtube.com/watch?v=corruptAAAA", Duration: 226}},
	}

	if _, err := step.Execute(context.Background(), ac, service.AfterSelect{}); err == nil {
		t.Fatal("expected an error when the only candidate is undecodable")
	}
	if ac.TempPath != "" {
		t.Errorf("TempPath must stay empty when the candidate is rejected, got %q", ac.TempPath)
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

func findLogRecord(t *testing.T, buf *logging.Capture, want string) map[string]any {
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

func logTestDownloadContext() *service.AcquisitionContext {
	return &service.AcquisitionContext{
		Track:  service.TrackRef{ID: logTestTrackID, Title: "Song", Artist: "Artist", CatalogDuration: 200},
		Ranked: []ports.AudioCandidate{logTestCandidate()},
	}
}

func TestCandidateLogs_StampTrackIDAndSource(t *testing.T) {
	identityCtx := func() *service.AcquisitionContext {
		ac := logTestDownloadContext()
		ac.Track.CatalogDuration = 0
		ac.Identity = ports.RecordingIdentity{MBID: "mb-want"}
		return ac
	}

	tests := []struct {
		name  string
		msg   string
		attrs map[string]any
		run   func(ctx context.Context, t *testing.T)
	}{
		{
			name: "search exclusion",
			msg:  "acquisition.candidate_excluded",
			run: func(ctx context.Context, t *testing.T) {
				c := logTestCandidate()
				other := ports.AudioCandidate{URL: "https://soundcloud.com/artist/other", Source: logTestSource}
				step := service.NewSearchStep(downloadtest.PlainFinder(&fakeAudioSearcher{searchResults: []ports.AudioCandidate{c, other}}))
				ac := &service.AcquisitionContext{
					Track:   service.TrackRef{ID: logTestTrackID, Title: "Song", Artist: "Artist"},
					Replace: service.ReplaceState{ExcludeKeys: []string{sourcekey.SourceKey(c.URL)}},
				}
				_, _ = step.Execute(ctx, ac, service.PipelineStart{})
			},
		},
		{
			name: "candidate evaluation",
			msg:  "candidate_evaluated",
			run: func(ctx context.Context, t *testing.T) {
				ac := &service.AcquisitionContext{Track: service.TrackRef{ID: logTestTrackID, Title: "Song", Artist: "Artist"}}
				searched, _ := service.NewSearchStep(downloadtest.PlainFinder(&fakeAudioSearcher{searchResults: []ports.AudioCandidate{logTestCandidate()}})).Execute(ctx, ac, service.PipelineStart{})
				_, _ = service.NewSelectStep().Execute(ctx, ac, searched)
			},
		},
		{
			name: "download failure",
			msg:  "acquisition.candidate_download_failed",
			run: func(ctx context.Context, t *testing.T) {
				step := newTestDownloadStep(&fileWritingSearcher{err: errors.New("fetch boom")})
				_, _ = step.Execute(ctx, logTestDownloadContext(), service.AfterSelect{})
			},
		},
		{
			name: "probe failure",
			msg:  "acquisition.probe_failed_accepting",
			run: func(ctx context.Context, t *testing.T) {
				runDownloadForLog(ctx, t, logTestDownloadContext(),
					withTestProber(scriptedProber{probeErr: errors.New("probe boom")}))
			},
		},
		{
			name:  "duration rejection",
			msg:   "acquisition.candidate_rejected_duration",
			attrs: map[string]any{"track_id": logTestTrackID, "source": logTestSource, "authoritative": false},
			run: func(ctx context.Context, t *testing.T) {
				runDownloadForLog(ctx, t, logTestDownloadContext(),
					withTestProber(scriptedProber{duration: 30}))
			},
		},
		{
			name:  "duration rejection against a resolved length",
			msg:   "acquisition.candidate_rejected_duration",
			attrs: map[string]any{"track_id": logTestTrackID, "source": logTestSource, "authoritative": true},
			run: func(ctx context.Context, t *testing.T) {
				ac := logTestDownloadContext()
				ac.Identity = ports.RecordingIdentity{Duration: 200}
				runDownloadForLog(ctx, t, ac, withTestProber(scriptedProber{duration: 30}))
			},
		},
		{
			name: "undecodable rejection",
			msg:  "acquisition.candidate_rejected_undecodable",
			run: func(ctx context.Context, t *testing.T) {
				runDownloadForLog(ctx, t, logTestDownloadContext(),
					withTestProber(scriptedProber{duration: 200, decodeErr: errors.New("decode boom")}))
			},
		},
		{
			name: "identify failure",
			msg:  "acquisition.identify_failed",
			run: func(ctx context.Context, t *testing.T) {
				runDownloadForLog(ctx, t, identityCtx(),
					withTestIdentifier(&stubIdentifier{err: errors.New("acoustid boom")}))
			},
		},
		{
			name: "identify unknown",
			msg:  "acquisition.identify_unknown",
			run: func(ctx context.Context, t *testing.T) {
				runDownloadForLog(ctx, t, identityCtx(),
					withTestIdentifier(&stubIdentifier{}))
			},
		},
		{
			name: "identify uncorroborated",
			msg:  "acquisition.identify_uncorroborated",
			run: func(ctx context.Context, t *testing.T) {
				runDownloadForLog(ctx, t, identityCtx(),
					withTestIdentifier(&stubIdentifier{match: ports.RecordingMatch{MBIDs: []string{"mb-other"}}}))
			},
		},
		{
			name: "contradicted rejection",
			msg:  "acquisition.audio_verdict",
			attrs: map[string]any{
				"track_id": logTestTrackID, "candidate_url": logTestCandidate().URL, "verdict": string(service.VerdictContradicted),
			},
			run: func(ctx context.Context, t *testing.T) {
				ac := identityCtx()
				ac.Ranked[0].Title = "Artist - Song (Live)"
				runDownloadForLog(ctx, t, ac,
					withTestIdentifier(&stubIdentifier{match: differentSongMatch()}))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs := logging.CaptureJSON(t, slog.LevelDebug)
			tt.run(context.Background(), t)

			rec := findLogRecord(t, logs, tt.msg)
			want := tt.attrs
			if want == nil {
				want = map[string]any{"track_id": logTestTrackID, "source": logTestSource}
			}
			for key, value := range want {
				if got := rec[key]; got != value {
					t.Errorf("%s %s = %v, want %v; record: %v", tt.msg, key, got, value, rec)
				}
			}
		})
	}
}

func runDownloadForLog(ctx context.Context, t *testing.T, ac *service.AcquisitionContext, opts ...any) {
	t.Helper()
	step := newTestDownloadStep(&fileWritingSearcher{writeFile: true}, opts...)
	_, _ = step.Execute(ctx, ac, service.AfterSelect{})
	if ac.TempPath != "" {
		_ = os.RemoveAll(filepath.Dir(ac.TempPath))
	}
}

type stubIdentifier struct {
	mu    sync.Mutex
	match ports.RecordingMatch
	err   error
	calls int
}

func (s *stubIdentifier) Identify(context.Context, string, float64) (ports.RecordingMatch, error) {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	return s.match, s.err
}

func downloadContext(mbid string) *service.AcquisitionContext {
	return &service.AcquisitionContext{
		Track:    service.TrackRef{Title: "Sunglasses at Night", Artist: "Corey Hart"},
		Identity: ports.RecordingIdentity{MBID: mbid},
		Ranked: []ports.AudioCandidate{
			{URL: "https://youtube.com/watch?v=first000000"},
			{URL: "https://youtube.com/watch?v=second00000"},
		},
	}
}

func referenceMatch() ports.RecordingMatch {
	return ports.RecordingMatch{
		AcoustID: "ac-master", MBIDs: []string{"mb-master"}, Score: 0.98,
		Results: linkedResults(ports.LinkedRecording{MBID: "mb-master", Title: "Sunglasses at Night", Artists: []string{"Corey Hart"}}),
	}
}

func differentSongMatch() ports.RecordingMatch {
	return ports.RecordingMatch{
		AcoustID: "ac-other", MBIDs: []string{"mb-other"}, Score: 0.98,
		Results: linkedResults(ports.LinkedRecording{MBID: "mb-other", Title: "Take On Me", Artists: []string{"a-ha"}}),
	}
}

func TestDownloadStep_RejectsAudioFingerprintedAsADifferentSong(t *testing.T) {
	identifier := &stubIdentifier{match: differentSongMatch()}
	step := newTestDownloadStep(&fileWritingSearcher{writeFile: true}, withTestIdentifier(identifier))
	ac := downloadContext("mb-master")
	for i := range ac.Ranked {
		ac.Ranked[i].Title = "Corey Hart - Sunglasses at Night (Live)"
	}

	_, err := step.Execute(context.Background(), ac, service.AfterSelect{})
	if err == nil {
		t.Fatal("expected every candidate rejected: the fingerprint links a different song and the title names another version")
	}
	if !strings.Contains(err.Error(), "contradicted") || !strings.Contains(err.Error(), "another recording") {
		t.Errorf("error = %q, want the contradiction naming the other recording", err)
	}
	if identifier.calls != 2 {
		t.Errorf("identify calls = %d, want both candidates fingerprinted and rejected", identifier.calls)
	}
	if ac.TempPath != "" {
		t.Errorf("TempPath must stay empty when the fingerprint rejects, got %q", ac.TempPath)
	}
}

func TestDownloadStep_AcceptsAudioFingerprintedAsTheReferenceRecording(t *testing.T) {
	identifier := &stubIdentifier{match: referenceMatch()}
	step := newTestDownloadStep(&fileWritingSearcher{writeFile: true}, withTestIdentifier(identifier))
	ac := downloadContext("mb-master")

	if _, err := step.Execute(context.Background(), ac, service.AfterSelect{}); err != nil {
		t.Fatalf("audio linked to the reference recording must be accepted: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(ac.TempPath))

	if !ac.IdentityVerified {
		t.Error("a Hard verdict is the strongest claim available and must mark the track verified")
	}
}

func TestDownloadStep_NeverRejectsAudioTheFingerprintCannotLink(t *testing.T) {
	identifier := &stubIdentifier{
		match: ports.RecordingMatch{AcoustID: "ac-unrelated", MBIDs: []string{"mb-unrelated"}},
	}
	step := newTestDownloadStep(&fileWritingSearcher{writeFile: true}, withTestIdentifier(identifier))
	ac := downloadContext("mb-master")
	ac.Track.CatalogDuration = 236
	ac.Ranked = []ports.AudioCandidate{{URL: "https://youtube.com/watch?v=first000000", Channel: "Corey Hart", Duration: 236}}

	if _, err := step.Execute(context.Background(), ac, service.AfterSelect{}); err != nil {
		t.Fatalf("an unlinked fingerprint is Unknown, never a rejection; the long tail must stay acquirable: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(ac.TempPath))

	if ac.IdentityVerified {
		t.Error("accepting without evidence must not claim verification")
	}
	if ac.Provenance() != downloadprovenance.ProvenanceBestEffort {
		t.Errorf("provenance = %q, want best_effort", ac.Provenance())
	}
}

func TestDownloadStep_UnknownAudioIsAccepted(t *testing.T) {
	identifier := &stubIdentifier{}
	step := newTestDownloadStep(&fileWritingSearcher{writeFile: true}, withTestIdentifier(identifier))
	ac := downloadContext("mb-master")
	ac.Track.CatalogDuration = 236
	ac.Ranked = []ports.AudioCandidate{{URL: "https://youtube.com/watch?v=first000000", Channel: "Corey Hart", Duration: 236}}

	if _, err := step.Execute(context.Background(), ac, service.AfterSelect{}); err != nil {
		t.Fatalf("AcoustID coverage is crowd-sourced; unknown audio must be accepted: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(ac.TempPath))

	if ac.Provenance() != downloadprovenance.ProvenanceBestEffort {
		t.Errorf("provenance = %q, want best_effort", ac.Provenance())
	}
}

func TestDownloadStep_IdentifierErrorIsFailOpen(t *testing.T) {
	identifier := &stubIdentifier{err: errors.New("acoustid down")}
	step := newTestDownloadStep(&fileWritingSearcher{writeFile: true}, withTestIdentifier(identifier))
	ac := downloadContext("mb-master")

	if _, err := step.Execute(context.Background(), ac, service.AfterSelect{}); err != nil {
		t.Fatalf("acquisition must never block on a broken validator: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(ac.TempPath))
}

func TestDownloadStep_NoExpectedMBIDSkipsIdentificationEntirely(t *testing.T) {
	identifier := &stubIdentifier{match: ports.RecordingMatch{AcoustID: "ac-whatever"}}
	step := newTestDownloadStep(&fileWritingSearcher{writeFile: true}, withTestIdentifier(identifier))
	ac := downloadContext("")

	if _, err := step.Execute(context.Background(), ac, service.AfterSelect{}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(ac.TempPath))

	if identifier.calls != 0 {
		t.Errorf("identify calls = %d, want 0 with nothing to compare against", identifier.calls)
	}
}

func TestDownloadStep_WidthOne_RejectionWalksToTheNextCandidate(t *testing.T) {
	identifier := &rejectFirstIdentifier{}
	step := newTestDownloadStep(&fileWritingSearcher{writeFile: true}, withTestIdentifier(identifier), service.WithVerifyWidth(1))
	ac := downloadContext("mb-master")

	if _, err := step.Execute(context.Background(), ac, service.AfterSelect{}); err != nil {
		t.Fatalf("a rejected candidate must not end the walk: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(ac.TempPath))

	if ac.Selected == nil || ac.Selected.URL != "https://youtube.com/watch?v=second00000" {
		t.Fatalf("selected = %+v, want the second candidate", ac.Selected)
	}
}

type rejectFirstIdentifier struct {
	calls int
}

func (r *rejectFirstIdentifier) Identify(context.Context, string, float64) (ports.RecordingMatch, error) {
	r.calls++
	if r.calls == 1 {
		return differentSongMatch(), nil
	}
	return referenceMatch(), nil
}

func linkedResults(recordings ...ports.LinkedRecording) []ports.AcoustIDResult {
	return []ports.AcoustIDResult{{ID: "ac-audio", Score: 0.97, Recordings: recordings}}
}

func TestDownloadStep_WidthOne_LiveOnlyLinksRejectedThenCleanCandidateVerified(t *testing.T) {
	live := ports.RecordingMatch{
		AcoustID: "ac-live", MBIDs: []string{"mb-live"}, Score: 0.97,
		Results: linkedResults(ports.LinkedRecording{MBID: "mb-live", Title: "Nessun dorma (live)"}),
	}
	clean := ports.RecordingMatch{
		AcoustID: "ac-studio", MBIDs: []string{"mb-studio"}, Score: 0.98,
		Results: linkedResults(ports.LinkedRecording{MBID: "mb-studio", Title: "Nessun dorma"}),
	}
	identifier := &sequenceIdentifier{matches: []ports.RecordingMatch{live, clean}}
	liveFacts := &stubVersionLookup{facts: map[string]ports.RecordingFacts{"mb-live": {MBID: "mb-live", Title: "Nessun dorma", Disambiguation: "live"}}}
	step := newTestDownloadStep(&fileWritingSearcher{writeFile: true}, withTestIdentifier(identifier), withTestLookup(liveFacts), service.WithVerifyWidth(1))
	ac := downloadContext("mb-studio")
	ac.Track.Title = "Nessun dorma"
	ac.Ranked[0].Title = "Nessun dorma (Live)"
	ac.Ranked[1].Title = "Nessun dorma"

	if _, err := step.Execute(context.Background(), ac, service.AfterSelect{}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(ac.TempPath))

	if len(ac.Rejections) != 1 || ac.Rejections[0].Stage != rejectedcandidates.Fingerprint || !strings.HasPrefix(ac.Rejections[0].Reason, "contradicted") {
		t.Errorf("rejections = %+v, want one fingerprint rejection as contradicted: the live link and the live title agree", ac.Rejections)
	}
	if ac.Provenance() != downloadprovenance.ProvenanceVerified || ac.Verdict.Kind != service.VerdictHard {
		t.Errorf("provenance = %v, verdict = %v, want verified and hard", ac.Provenance(), ac.Verdict.Kind)
	}
}

func TestDownloadStep_LinksToAnotherSongAreRejected(t *testing.T) {
	match := ports.RecordingMatch{
		AcoustID: "ac-x", MBIDs: []string{"mb-x"}, Score: 0.9,
		Results: linkedResults(ports.LinkedRecording{MBID: "mb-x", Title: "Something Else Entirely"}),
	}
	step := newTestDownloadStep(&fileWritingSearcher{writeFile: true}, withTestIdentifier(&stubIdentifier{match: match}))
	ac := downloadContext("mb-master")
	for i := range ac.Ranked {
		ac.Ranked[i].Title = "Corey Hart - Sunglasses at Night (Live)"
	}

	if _, err := step.Execute(context.Background(), ac, service.AfterSelect{}); err == nil {
		t.Fatal("expected every candidate rejected: another song linked and another version titled")
	}
	if len(ac.Rejections) == 0 || !strings.HasPrefix(ac.Rejections[0].Reason, "contradicted") {
		t.Errorf("rejections = %+v, want a contradicted reason", ac.Rejections)
	}
}

func TestDownloadStep_LogsAudioVerdictForEveryDecision(t *testing.T) {
	logs := logging.CaptureJSON(t, slog.LevelDebug)
	match := ports.RecordingMatch{
		AcoustID: "ac-a", MBIDs: []string{"mb-master"}, Score: 0.97,
		Results: linkedResults(ports.LinkedRecording{MBID: "mb-master", Title: "Sunglasses at Night"}),
	}
	step := newTestDownloadStep(&fileWritingSearcher{writeFile: true}, withTestIdentifier(&stubIdentifier{match: match}))
	ac := downloadContext("mb-master")

	if _, err := step.Execute(context.Background(), ac, service.AfterSelect{}); err != nil {
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

func previewMatch(mbid, title string) ports.RecordingMatch {
	return ports.RecordingMatch{
		AcoustID: "ac-" + mbid, MBIDs: []string{mbid}, Score: 0.98,
		Results: linkedResults(ports.LinkedRecording{MBID: mbid, Title: title}),
	}
}

func previewContext() *service.AcquisitionContext {
	ac := downloadContext("mb-studio")
	ac.Track.Title = "Nessun dorma"
	ac.Ranked = ac.Ranked[:1]
	ac.Ranked[0].Duration = 250
	return ac
}

func TestDownloadStep_PreviewOtherVersionRejectsWithoutFullFetch(t *testing.T) {
	fetcher := &previewingFetcher{fileWritingSearcher: fileWritingSearcher{writeFile: true}}
	identifier := &durationRecordingIdentifier{match: previewMatch("mb-live", "Nessun dorma (live)")}
	liveFacts := &stubVersionLookup{facts: map[string]ports.RecordingFacts{"mb-live": {MBID: "mb-live", Title: "Nessun dorma", Disambiguation: "live"}}}
	step := newTestDownloadStep(fetcher, withTestIdentifier(identifier), withTestLookup(liveFacts))
	ac := previewContext()
	ac.Ranked[0].Title = "Nessun dorma (Live)"

	if _, err := step.Execute(context.Background(), ac, service.AfterSelect{}); err == nil {
		t.Fatal("expected the live preview to reject the only candidate")
	}

	if fetcher.calls != 0 {
		t.Errorf("full fetches = %d, want 0 after a rejecting preview", fetcher.calls)
	}
	if len(ac.Rejections) != 1 || !strings.HasPrefix(ac.Rejections[0].Reason, "contradicted") {
		t.Errorf("rejections = %+v, want one contradicted rejection", ac.Rejections)
	}
}

func TestDownloadStep_AcceptedPreviewIsFollowedByOneFullFetchAndNoSecondIdentify(t *testing.T) {
	fetcher := &previewingFetcher{fileWritingSearcher: fileWritingSearcher{writeFile: true}}
	identifier := &durationRecordingIdentifier{match: previewMatch("mb-studio", "Nessun dorma")}
	step := newTestDownloadStep(fetcher, withTestIdentifier(identifier))
	ac := previewContext()

	if _, err := step.Execute(context.Background(), ac, service.AfterSelect{}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(ac.TempPath))

	if fetcher.calls != 1 || len(identifier.durations) != 1 {
		t.Errorf("full fetches = %d, identify calls = %d, want 1 and 1", fetcher.calls, len(identifier.durations))
	}
	if ac.Verdict.Kind != service.VerdictHard || !ac.IdentityVerified {
		t.Errorf("verdict = %q identity = %v, want the preview's hard verdict carried over", ac.Verdict.Kind, ac.IdentityVerified)
	}
	if _, err := os.Stat(fetcher.previewDirs[0]); !os.IsNotExist(err) {
		t.Errorf("preview dir still present: %v", err)
	}
}

func TestDownloadStep_FailedPreviewFallsBackToTheFullPath(t *testing.T) {
	fetcher := &previewingFetcher{fileWritingSearcher: fileWritingSearcher{writeFile: true}, previewErr: errors.New("boom")}
	identifier := &durationRecordingIdentifier{match: previewMatch("mb-studio", "Nessun dorma")}
	step := newTestDownloadStep(fetcher, withTestIdentifier(identifier))
	ac := previewContext()

	if _, err := step.Execute(context.Background(), ac, service.AfterSelect{}); err != nil {
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
	step := newTestDownloadStep(fetcher, withTestIdentifier(identifier))
	ac := previewContext()

	if _, err := step.Execute(context.Background(), ac, service.AfterSelect{}); err != nil {
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
	finished chan string
	inFlight atomic.Int32
	peak     atomic.Int32
}

func newGatedFetcher(gates map[string]chan struct{}) *gatedFetcher {
	return &gatedFetcher{dirs: map[string]string{}, gates: gates, started: make(chan string, 64), finished: make(chan string, 64)}
}

func (f *gatedFetcher) Fetch(_ context.Context, c ports.AudioCandidate, outDir string) (ports.FetchedAudio, error) {
	now := f.inFlight.Add(1)
	defer func() {
		f.inFlight.Add(-1)
		f.finished <- c.URL
	}()
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
	return fetchedFrom(path, os.WriteFile(path, []byte("audio"), 0o644))
}

func (f *gatedFetcher) dirOf(url string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.dirs[url]
}

func TestDownloadStep_WindowedKeepsBestRankedAcceptedEvenWhenLowerRankFinishesFirst(t *testing.T) {
	rank1Gate := make(chan struct{})
	fetcher := newGatedFetcher(map[string]chan struct{}{"rank1": rank1Gate})
	ac := &service.AcquisitionContext{Ranked: []ports.AudioCandidate{{URL: "rank1"}, {URL: "rank2"}}}
	done := make(chan error, 1)

	go func() {
		_, err := newTestDownloadStep(fetcher, service.WithVerifyWidth(2)).Execute(context.Background(), ac, service.AfterSelect{})
		done <- err
	}()
	for range 2 {
		<-fetcher.started
	}
	<-fetcher.finished
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
	synctest.Test(t, func(t *testing.T) { sharedLimiterCapsInFlightFetches(t) })
}

func sharedLimiterCapsInFlightFetches(t *testing.T) {
	gate := make(chan struct{})
	fetcher := newGatedFetcher(map[string]chan struct{}{"a": gate, "b": gate})
	limiter := service.MustLimitedTo(6)
	var wg sync.WaitGroup
	execute := func(step *service.DownloadStep, urls ...string) {
		defer wg.Done()
		ranked := make([]ports.AudioCandidate, 0, len(urls))
		for _, u := range urls {
			ranked = append(ranked, ports.AudioCandidate{URL: u})
		}
		ac := &service.AcquisitionContext{Ranked: ranked}
		if _, err := step.Execute(context.Background(), ac, service.AfterSelect{}); err == nil {
			os.RemoveAll(filepath.Dir(ac.TempPath))
		}
	}

	wg.Add(3)
	for range 3 {
		go execute(newTestDownloadStep(fetcher, service.WithVerifyWidth(2), withTestLimit(limiter)), "a", "b")
	}
	for range 6 {
		<-fetcher.started
	}
	wg.Add(1)
	go execute(newTestDownloadStep(fetcher, withTestLimit(limiter)), "seventh")

	synctest.Wait()
	select {
	case <-fetcher.started:
		t.Fatal("seventh fetch started while six were in flight")
	default:
	}
	close(gate)
	wg.Wait()

	if peak := fetcher.peak.Load(); peak > 6 {
		t.Errorf("peak in-flight fetches = %d, want <= 6", peak)
	}
}

type gateRecorder struct {
	mu    sync.Mutex
	gates []string
}

func (r *gateRecorder) RecordVerifySkip(gate string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.gates = append(r.gates, gate)
}

func assertWindowSkips(t *testing.T, gates []string, want string) {
	t.Helper()
	if len(gates) < 1 || len(gates) > 2 {
		t.Fatalf("recorded gates = %v, want one %s per attempt that ran (1 winner, plus the discarded window candidate if not yet cancelled)", gates, want)
	}
	for _, g := range gates {
		if g != want {
			t.Errorf("recorded gates = %v, want only %s", gates, want)
		}
	}
}

func TestDownloadStep_IdentifierErrorRecordsAnIdentifyFailedSkip(t *testing.T) {
	rec := &gateRecorder{}
	identifier := &stubIdentifier{err: errors.New("acoustid down")}
	step := newTestDownloadStep(&fileWritingSearcher{writeFile: true},
		withTestIdentifier(identifier), withTestSkips(rec))
	ac := downloadContext("mb-master")
	ac.Track.CatalogDuration = 200

	if _, err := step.Execute(context.Background(), ac, service.AfterSelect{}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(ac.TempPath))

	assertWindowSkips(t, rec.gates, "identify_failed")
}

func TestDownloadStep_FailedPreviewIdentifyRecordsAPreviewFallbackSkip(t *testing.T) {
	rec := &gateRecorder{}
	fetcher := &previewingFetcher{fileWritingSearcher: fileWritingSearcher{writeFile: true}, previewErr: errors.New("boom")}
	identifier := &durationRecordingIdentifier{match: previewMatch("mb-studio", "Nessun dorma")}
	step := newTestDownloadStep(fetcher, withTestIdentifier(identifier), withTestSkips(rec))
	ac := previewContext()
	ac.Track.CatalogDuration = 250

	if _, err := step.Execute(context.Background(), ac, service.AfterSelect{}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(ac.TempPath))

	if len(rec.gates) != 1 || rec.gates[0] != "preview_fallback" {
		t.Errorf("recorded gates = %v, want [preview_fallback]", rec.gates)
	}
}

func TestDownloadStep_ThrottledIdentifyLogsThrottledAndLeavesIdentityUnverified(t *testing.T) {
	logs := logging.CaptureJSON(t, slog.LevelDebug)
	ac := logTestDownloadContext()
	ac.Track.CatalogDuration = 0
	ac.Identity = ports.RecordingIdentity{MBID: "mb-want"}
	throttled := fmt.Errorf("acoustid lookup: %w", acqdomain.ErrIdentifyThrottled)

	runDownloadForLog(context.Background(), t, ac,
		withTestIdentifier(&stubIdentifier{err: throttled}))

	findLogRecord(t, logs, "acquisition.identify_throttled")
	if strings.Contains(logs.String(), "acquisition.identify_failed") {
		t.Errorf("throttle logged as identify_failed: %s", logs.String())
	}
	if ac.IdentityVerified {
		t.Error("IdentityVerified = true, want false after a throttled lookup")
	}
}

func TestDownloadStep_ThrottledPreviewLogsThrottledNotPreviewFallback(t *testing.T) {
	logs := logging.CaptureJSON(t, slog.LevelDebug)
	fetcher := &previewingFetcher{fileWritingSearcher: fileWritingSearcher{writeFile: true}}
	identifier := &stubIdentifier{err: fmt.Errorf("acoustid lookup: %w", acqdomain.ErrIdentifyThrottled)}
	step := newTestDownloadStep(fetcher, withTestIdentifier(identifier))
	ac := previewContext()

	_, _ = step.Execute(context.Background(), ac, service.AfterSelect{})
	if ac.TempPath != "" {
		_ = os.RemoveAll(filepath.Dir(ac.TempPath))
	}

	findLogRecord(t, logs, "acquisition.identify_throttled")
	if strings.Contains(logs.String(), "acquisition.preview_fallback") {
		t.Errorf("throttle logged as preview_fallback: %s", logs.String())
	}
}

type nestedLayoutFetcher struct{}

func (nestedLayoutFetcher) Fetch(_ context.Context, _ ports.AudioCandidate, outDir string) (ports.FetchedAudio, error) {
	dir := filepath.Join(outDir, "Artist", "Album")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fetchedFrom("", err)
	}
	path := filepath.Join(dir, "track.flac")
	return fetchedFrom(path, os.WriteFile(path, []byte("audio-bytes"), 0o644))
}

func TestDownloadStep_Execute_NestedLayout_RollbackLeavesNoTempRoot(t *testing.T) {
	step := newTestDownloadStep(nestedLayoutFetcher{})
	ac := &service.AcquisitionContext{Ranked: []ports.AudioCandidate{{URL: "https://example.com/x"}}}

	if _, err := step.Execute(context.Background(), ac, service.AfterSelect{}); err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	root := ac.TempDir
	if !strings.HasPrefix(filepath.Base(root), ports.TempDirPrefix) || filepath.Dir(root) != os.TempDir() {
		t.Fatalf("TempDir = %q, want the %s* MkdirTemp root", root, ports.TempDirPrefix)
	}

	if err := step.Rollback(context.Background(), ac); err != nil {
		t.Fatalf("Rollback error: %v", err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Errorf("temp root %q should be gone after rollback, stat err = %v", root, err)
	}
}

type findOnlySource struct {
	name       acqdomain.SourceName
	candidates []ports.AudioCandidate
	err        error
	fetchErr   error
}

func (s findOnlySource) Name() acqdomain.SourceName { return s.name }

func (s findOnlySource) Find(context.Context, ports.FindRequest) ([]ports.AudioCandidate, error) {
	return s.candidates, s.err
}

func (s findOnlySource) Fetch(context.Context, ports.AudioCandidate, string) (ports.FetchedAudio, error) {
	return fetchedFrom("", s.fetchErr)
}

func searchThenDownloadRejectingAll(t *testing.T, down findOnlySource) (ports.AcquisitionJob, error) {
	t.Helper()
	healthy := findOnlySource{
		name:       "healthy",
		candidates: []ports.AudioCandidate{{URL: "https://example.com/a"}},
		fetchErr:   errors.New("exit 1"),
	}
	registry := service.NewSourceRegistry(down, healthy)
	ctx := context.Background()
	job := ports.AcquisitionJob{Attempt: 1}
	ac := &service.AcquisitionContext{Job: job}
	if _, err := service.NewSearchStep(registry).Execute(ctx, ac, service.PipelineStart{}); err != nil {
		t.Fatalf("search: %v", err)
	}
	ac.Ranked = ac.Candidates
	_, err := newTestDownloadStep(registry).Execute(ctx, ac, service.AfterSelect{})
	if err == nil {
		t.Fatal("expected a download error")
	}
	return job, err
}

func TestDownloadStep_SearchOutageWithAllSurvivorsRejected_IsRetryable(t *testing.T) {
	down := findOnlySource{name: "down", err: &acqdomain.SourceUnavailableError{Source: "down", Err: errors.New("429")}}

	job, err := searchThenDownloadRejectingAll(t, down)

	if !acqdomain.IsSourceUnavailable(err) || !service.WillRetry(job, err) {
		t.Fatalf("err = %v, want a retryable source-unavailable failure", err)
	}
}

func TestDownloadStep_HealthySourcesAllRejected_StaysPermanent(t *testing.T) {
	quiet := findOnlySource{name: "quiet"}

	job, err := searchThenDownloadRejectingAll(t, quiet)

	if acqdomain.IsSourceUnavailable(err) || service.WillRetry(job, err) {
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

func hardMatch() ports.RecordingMatch {
	return ports.RecordingMatch{
		AcoustID: "ac-a", MBIDs: []string{"mb-master"}, Score: 0.97,
		Results: linkedResults(ports.LinkedRecording{MBID: "mb-master", Title: "Sunglasses at Night"}),
	}
}

func heldTrackContext(ranked ...ports.AudioCandidate) *service.AcquisitionContext {
	return &service.AcquisitionContext{
		Track:    service.TrackRef{Title: "Sunglasses at Night", Artist: "Corey Hart", CatalogDuration: 236},
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
			step := newTestDownloadStep(fetcher, withTestIdentifier(identifier), service.WithVerifyWidth(width))

			if _, err := step.Execute(context.Background(), ac, service.AfterSelect{}); err != nil {
				t.Fatalf("Execute: %v", err)
			}
			defer os.RemoveAll(filepath.Dir(ac.TempPath))

			if ac.Selected == nil || ac.Selected.URL != "hard" {
				t.Fatalf("selected = %+v, want the hard match", ac.Selected)
			}
			if got := ac.Provenance(); got != downloadprovenance.ProvenanceVerified {
				t.Errorf("provenance = %q, want verified", got)
			}
			if _, err := os.Stat(fetcher.dirOf("unknown")); !os.IsNotExist(err) {
				t.Errorf("the held unknown candidate's temp dir should be removed, stat err = %v", err)
			}
		})
	}
}

func TestDownloadStep_CleanHoldBeatsAHigherRankedOtherVersionHold(t *testing.T) {
	fetcher := newGatedFetcher(nil)
	identifier := &urlIdentifier{fetcher: fetcher, matches: map[string]ports.RecordingMatch{"edit": hardMatch()}}
	ac := heldTrackContext(
		ports.AudioCandidate{URL: "edit", Title: "Sunglasses at Night (Radio Edit)", Channel: "Corey Hart - Topic", Duration: 236},
		ports.AudioCandidate{URL: "clean", Title: "Sunglasses at Night", Channel: "Corey Hart - Topic", Duration: 236},
	)
	step := newTestDownloadStep(fetcher, withTestIdentifier(identifier))

	if _, err := step.Execute(context.Background(), ac, service.AfterSelect{}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(ac.TempPath))

	if ac.Selected == nil || ac.Selected.URL != "clean" {
		t.Fatalf("selected = %+v, want the clean hold", ac.Selected)
	}
	if !ac.BestEffort || ac.Provenance() != downloadprovenance.ProvenanceBestEffort {
		t.Errorf("a held selection must be best_effort, got BestEffort=%v provenance=%q", ac.BestEffort, ac.Provenance())
	}
	if _, err := os.Stat(fetcher.dirOf("edit")); !os.IsNotExist(err) {
		t.Errorf("the displaced other-version hold's temp dir should be removed, stat err = %v", err)
	}
}

func TestDownloadStep_HeldCandidateBelowTheFloorFailsAsNoConfidentMatch(t *testing.T) {
	fetcher := newGatedFetcher(nil)
	ac := heldTrackContext(ports.AudioCandidate{URL: "reupload", Channel: "chill uploads", Duration: 256})
	step := newTestDownloadStep(fetcher, withTestIdentifier(&urlIdentifier{fetcher: fetcher}))

	_, err := step.Execute(context.Background(), ac, service.AfterSelect{})

	if !errors.Is(err, service.ErrNoConfidentMatch) {
		t.Fatalf("err = %v, want ErrNoConfidentMatch", err)
	}
	if _, statErr := os.Stat(fetcher.dirOf("reupload")); !os.IsNotExist(statErr) {
		t.Errorf("the refused hold's temp dir should be removed, stat err = %v", statErr)
	}
}

func TestDownloadStep_OtherVersionOfTheWrongLengthIsNeverStored(t *testing.T) {
	fetcher := newGatedFetcher(nil)
	ac := heldTrackContext(ports.AudioCandidate{
		URL: "edit", Title: "Sunglasses at Night (Radio Edit)", Channel: "Corey Hart - Topic", Duration: 236,
	})
	prober := scriptedProber{duration: 245}
	step := newTestDownloadStep(fetcher, withTestProber(prober))

	_, err := step.Execute(context.Background(), ac, service.AfterSelect{})

	if !errors.Is(err, service.ErrNoConfidentMatch) {
		t.Fatalf("err = %v, want ErrNoConfidentMatch", err)
	}
}

func TestDownloadStep_EveryCandidateImplausibleFailsAsNoConfidentMatch(t *testing.T) {
	ac := heldTrackContext(ports.AudioCandidate{URL: "far", Duration: 100})
	step := newTestDownloadStep(&fileWritingSearcher{writeFile: true})

	_, err := step.Execute(context.Background(), ac, service.AfterSelect{})

	if !errors.Is(err, service.ErrNoConfidentMatch) {
		t.Fatalf("err = %v, want ErrNoConfidentMatch", err)
	}
}

type panickingFetcher struct {
	inner *gatedFetcher
	url   string
}

func (f *panickingFetcher) Fetch(ctx context.Context, c ports.AudioCandidate, outDir string) (ports.FetchedAudio, error) {
	if c.URL == f.url {
		f.inner.mu.Lock()
		f.inner.dirs[c.URL] = outDir
		f.inner.mu.Unlock()
		panic("fetcher exploded")
	}
	return f.inner.Fetch(ctx, c, outDir)
}

func TestDownloadStep_WindowedPanickingAttemptIsRejectedAndOthersStillWin(t *testing.T) {
	inner := newGatedFetcher(nil)
	fetcher := &panickingFetcher{inner: inner, url: "rank1"}
	ac := &service.AcquisitionContext{Ranked: []ports.AudioCandidate{{URL: "rank1"}, {URL: "rank2"}}}

	_, err := newTestDownloadStep(fetcher, service.WithVerifyWidth(2)).Execute(context.Background(), ac, service.AfterSelect{})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(ac.TempPath))
	if ac.Selected == nil || ac.Selected.URL != "rank2" {
		t.Fatalf("selected = %+v, want rank2", ac.Selected)
	}
	if _, statErr := os.Stat(inner.dirOf("rank1")); !os.IsNotExist(statErr) {
		t.Errorf("panicked attempt's temp dir should be removed, stat err = %v", statErr)
	}
}

func TestDownloadStep_WindowedAllAttemptsPanickingReturnsErrorNotCrash(t *testing.T) {
	inner := newGatedFetcher(nil)
	fetcher := &panickingFetcher{inner: inner, url: "only"}
	ac := &service.AcquisitionContext{Ranked: []ports.AudioCandidate{{URL: "only"}}}

	_, err := newTestDownloadStep(fetcher, service.WithVerifyWidth(2)).Execute(context.Background(), ac, service.AfterSelect{})

	if err == nil {
		t.Fatal("Execute should fail when every attempt panics")
	}
	if _, statErr := os.Stat(inner.dirOf("only")); !os.IsNotExist(statErr) {
		t.Errorf("temp dir should be removed, stat err = %v", statErr)
	}
	if ac.TempPath != "" {
		t.Errorf("TempPath = %q, want it empty when every attempt panicked", ac.TempPath)
	}
}

func TestDownloadStep_ProbeErrorIsAcceptedAndRecordsAProbeFailedSkip(t *testing.T) {
	rec := &gateRecorder{}
	step := newTestDownloadStep(&fileWritingSearcher{writeFile: true},
		withTestProber(scriptedProber{probeErr: errors.New("probe boom")}), withTestSkips(rec))
	ac := downloadContext("")
	ac.Track.CatalogDuration = 200

	if _, err := step.Execute(context.Background(), ac, service.AfterSelect{}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(ac.TempPath))

	if ac.TempPath == "" {
		t.Error("probe error should still accept the candidate")
	}
	assertWindowSkips(t, rec.gates, ports.SkipProbeFailed)
}

func TestDownloadStep_AProbeNoRecordingCanHaveIsAProbeFailureAndNothingIsRecordedAsTheLength(t *testing.T) {
	for name, probed := range map[string]float64{"above seven days": 8 * 24 * 60 * 60, "negative": -3, "not a number": math.NaN()} {
		t.Run(name, func(t *testing.T) {
			rec := &gateRecorder{}
			step := newTestDownloadStep(&fileWritingSearcher{writeFile: true},
				withTestProber(scriptedProber{duration: probed}), withTestSkips(rec))
			ac := downloadContext("")
			ac.Track.CatalogDuration = 200

			if _, err := step.Execute(context.Background(), ac, service.AfterSelect{}); err != nil {
				t.Fatalf("Execute: %v", err)
			}
			defer os.RemoveAll(filepath.Dir(ac.TempPath))

			if ac.TempPath == "" {
				t.Error("an implausible probe is a probe failure, which still accepts the candidate")
			}
			if ac.ProbedDuration.Known() || ac.DurationVerified {
				t.Errorf("probed length = %v s, duration verified = %v, want neither recorded", ac.ProbedDuration.Seconds(), ac.DurationVerified)
			}
			assertWindowSkips(t, rec.gates, ports.SkipProbeFailed)
		})
	}
}

func TestDownloadStep_APlausibleProbeIsRecordedAsTheProbedLength(t *testing.T) {
	step := newTestDownloadStep(&fileWritingSearcher{writeFile: true}, withTestProber(scriptedProber{duration: 201.5}))
	ac := downloadContext("")
	ac.Track.CatalogDuration = 200

	if _, err := step.Execute(context.Background(), ac, service.AfterSelect{}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(ac.TempPath))

	if got := ac.ProbedDuration.Seconds(); got != 201.5 {
		t.Errorf("probed length = %v s, want 201.5", got)
	}
}

func TestDownloadStep_UnknownDurationIsAcceptedAndRecordsANoDurationSkip(t *testing.T) {
	rec := &gateRecorder{}
	step := newTestDownloadStep(&fileWritingSearcher{writeFile: true},
		withTestProber(scriptedProber{duration: 200}), withTestSkips(rec))
	ac := downloadContext("")
	ac.Track.CatalogDuration = 0

	if _, err := step.Execute(context.Background(), ac, service.AfterSelect{}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(ac.TempPath))

	if ac.TempPath == "" {
		t.Error("unknown duration should still accept the candidate")
	}
	assertWindowSkips(t, rec.gates, ports.SkipNoDuration)
}

func TestDownloadStep_SuccessfulProbeRecordsNoSkip(t *testing.T) {
	rec := &gateRecorder{}
	step := newTestDownloadStep(&fileWritingSearcher{writeFile: true},
		withTestProber(scriptedProber{duration: 200}), withTestSkips(rec))
	ac := downloadContext("")
	ac.Track.CatalogDuration = 200

	if _, err := step.Execute(context.Background(), ac, service.AfterSelect{}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(ac.TempPath))

	if len(rec.gates) != 0 {
		t.Errorf("recorded gates = %v, want none", rec.gates)
	}
}

type scriptedSourceFetcher struct {
	hangURL     string
	unavailable acqdomain.SourceName
	mu          sync.Mutex
	calls       map[acqdomain.SourceName]int
}

func (f *scriptedSourceFetcher) Fetch(ctx context.Context, c ports.AudioCandidate, outDir string) (ports.FetchedAudio, error) {
	f.mu.Lock()
	f.calls[c.Source]++
	f.mu.Unlock()
	if c.URL == f.hangURL {
		<-ctx.Done()
		return fetchedFrom("", ctx.Err())
	}
	if c.Source == f.unavailable {
		return fetchedFrom("", &acqdomain.SourceUnavailableError{Source: c.Source, Err: errors.New("HTTP Error 429")})
	}
	path := filepath.Join(outDir, "track.mp3")
	return fetchedFrom(path, os.WriteFile(path, []byte("audio-bytes"), 0o644))
}

func TestDownloadStep_HungFirstCandidateDoesNotStarveFallbackSource(t *testing.T) {
	fetcher := &scriptedSourceFetcher{hangURL: "u1", calls: map[acqdomain.SourceName]int{}}
	ac := &service.AcquisitionContext{Ranked: []ports.AudioCandidate{
		{URL: "u1", Source: "ytmusic"},
		{URL: "u2", Source: "ytdlp"},
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := newTestDownloadStep(fetcher, service.WithCandidateTimeout(50*time.Millisecond)).Execute(ctx, ac, service.AfterSelect{})
	if err != nil {
		t.Fatalf("Execute = %v, want the second source to be stored", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(ac.TempDir) })
	if ac.Selected == nil || ac.Selected.URL != "u2" {
		t.Fatalf("selected = %+v, want u2", ac.Selected)
	}
}

func TestDownloadStep_EveryCandidateTimingOutIsTransient(t *testing.T) {
	fetcher := &scriptedSourceFetcher{hangURL: "u1", calls: map[acqdomain.SourceName]int{}}
	ac := &service.AcquisitionContext{Ranked: []ports.AudioCandidate{{URL: "u1", Source: "ytmusic"}}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := newTestDownloadStep(fetcher, service.WithCandidateTimeout(20*time.Millisecond)).Execute(ctx, ac, service.AfterSelect{})

	if code := failureCodeOf(&service.StepError{Step: service.StepNameDownload, Err: err}); code != downloadfailure.FailureSourceUnavailable {
		t.Fatalf("failure code = %s, want %s (err=%v)", code, downloadfailure.FailureSourceUnavailable, err)
	}
	if !service.WillRetry(ports.AcquisitionJob{Attempt: 1}, err) {
		t.Fatalf("willRetry = false for %v", err)
	}
}

func TestDownloadStep_UnavailableSourceIsFetchedOncePerJob(t *testing.T) {
	for _, width := range []int{1, 2} {
		t.Run(fmt.Sprintf("width%d", width), func(t *testing.T) {
			fetcher := &scriptedSourceFetcher{unavailable: "ytdlp", calls: map[acqdomain.SourceName]int{}}
			ranked := make([]ports.AudioCandidate, 8)
			for i := range ranked {
				ranked[i] = ports.AudioCandidate{URL: fmt.Sprintf("u%d", i), Source: "ytdlp"}
			}
			ac := &service.AcquisitionContext{Ranked: ranked}

			_, err := newTestDownloadStep(fetcher, service.WithVerifyWidth(width)).Execute(context.Background(), ac, service.AfterSelect{})

			if code := failureCodeOf(&service.StepError{Step: service.StepNameDownload, Err: err}); code != downloadfailure.FailureSourceUnavailable {
				t.Fatalf("failure code = %s, want %s", code, downloadfailure.FailureSourceUnavailable)
			}
			want := width
			if got := fetcher.calls["ytdlp"]; got != want {
				t.Errorf("fetch calls = %d, want %d", got, want)
			}
		})
	}
}

func TestDownloadStep_WorkspaceRejectedCandidateLeavesNoLiveDir(t *testing.T) {
	ws := newCreationRecordingWorkspace()
	step := mustNewDownloadStep(&fileWritingSearcher{err: errors.New("boom")}, ws, ports.NoopAudioIdentifier(), ports.NoopRecordingLookup(), downloadtest.SilentAudioProber(), ports.NoopVerifySkipRecorder(), service.Unlimited())
	ac := &service.AcquisitionContext{Ranked: []ports.AudioCandidate{{URL: "https://example.com/x"}}}

	if _, err := step.Execute(context.Background(), ac, service.AfterSelect{}); err == nil {
		t.Fatal("expected download error")
	}

	if len(ws.createdDirs()) != 1 {
		t.Fatalf("created = %v, want one dir", ws.createdDirs())
	}
	if live := ws.liveDirs(); len(live) != 0 {
		t.Errorf("live dirs after rejection = %v, want none", live)
	}
}

func TestDownloadStep_WorkspaceWindowRemovesLoserAndKeepsWinnerUntilCleanup(t *testing.T) {
	ws := newCreationRecordingWorkspace()
	rank1Gate := make(chan struct{})
	fetcher := newGatedFetcher(map[string]chan struct{}{"rank1": rank1Gate})
	ac := &service.AcquisitionContext{Ranked: []ports.AudioCandidate{{URL: "rank1"}, {URL: "rank2"}}}
	done := make(chan error, 1)

	go func() {
		_, err := mustNewDownloadStep(fetcher, ws, ports.NoopAudioIdentifier(), ports.NoopRecordingLookup(), downloadtest.SilentAudioProber(), ports.NoopVerifySkipRecorder(), service.Unlimited(), service.WithVerifyWidth(2)).Execute(context.Background(), ac, service.AfterSelect{})
		done <- err
	}()
	for range 2 {
		<-fetcher.started
	}
	<-fetcher.finished
	close(rank1Gate)
	if err := <-done; err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	winner, loser := fetcher.dirOf("rank1"), fetcher.dirOf("rank2")
	if ac.TempDir != winner {
		t.Fatalf("TempDir = %q, want winner dir %q", ac.TempDir, winner)
	}
	if ws.removedCount(loser) != 1 {
		t.Errorf("loser dir removed %d times during the step, want 1", ws.removedCount(loser))
	}
	if ws.removedCount(winner) != 0 {
		t.Errorf("winner dir removed during the step")
	}
	if _, err := os.Stat(winner); err != nil {
		t.Errorf("winner dir should survive until cleanup: %v", err)
	}

	service.CleanupTemp(context.Background(), ws, ac)

	if ws.removedCount(winner) != 1 {
		t.Errorf("winner dir removed %d times by CleanupTemp, want 1", ws.removedCount(winner))
	}
	if _, err := os.Stat(winner); !os.IsNotExist(err) {
		t.Errorf("winner dir should be gone after CleanupTemp, stat err = %v", err)
	}
}

func TestDownloadStep_WorkspacePreviewDirCreatedAndRemovedAfterIdentify(t *testing.T) {
	ws := newCreationRecordingWorkspace()
	fetcher := &previewingFetcher{fileWritingSearcher: fileWritingSearcher{writeFile: true}}
	identifier := &durationRecordingIdentifier{match: previewMatch("mb-live", "Nessun dorma (live)")}
	step := mustNewDownloadStep(fetcher, ws, identifier, ports.NoopRecordingLookup(), downloadtest.SilentAudioProber(), ports.NoopVerifySkipRecorder(), service.Unlimited())

	if _, err := step.Execute(context.Background(), previewContext(), service.AfterSelect{}); err == nil {
		t.Fatal("expected the live preview to reject the only candidate")
	}

	if len(fetcher.previewDirs) != 1 {
		t.Fatalf("preview dirs = %v, want one", fetcher.previewDirs)
	}
	preview := fetcher.previewDirs[0]
	if !strings.HasPrefix(filepath.Base(preview), ports.TempDirPrefix+"preview-") {
		t.Errorf("preview dir %q, want a %spreview-* dir", preview, ports.TempDirPrefix)
	}
	if ws.removedCount(preview) != 1 {
		t.Errorf("preview dir removed %d times, want 1", ws.removedCount(preview))
	}
	if _, err := os.Stat(preview); !os.IsNotExist(err) {
		t.Errorf("preview dir should be gone, stat err = %v", err)
	}
}

func TestDownloadStep_WorkspaceRollbackWithOnlyTempPathRemovesThatFile(t *testing.T) {
	ws := newCreationRecordingWorkspace()
	file := filepath.Join(t.TempDir(), "track.mp3")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	step := mustNewDownloadStep(&fileWritingSearcher{}, ws, ports.NoopAudioIdentifier(), ports.NoopRecordingLookup(), downloadtest.SilentAudioProber(), ports.NoopVerifySkipRecorder(), service.Unlimited())
	if err := step.Rollback(context.Background(), &service.AcquisitionContext{TempPath: file}); err != nil {
		t.Fatalf("Rollback error: %v", err)
	}

	if got := ws.removedPaths(); len(got) != 1 || got[0] != file {
		t.Errorf("removed = %v, want exactly [%s]", got, file)
	}
}

func assertNoCookiePathInText(t *testing.T, logs string, wantMsg string) {
	t.Helper()
	if !strings.Contains(logs, wantMsg) {
		t.Fatalf("expected log %q, got:\n%s", wantMsg, logs)
	}
	if strings.Contains(logs, secretCookiePath) || strings.Contains(logs, "/run/secrets") {
		t.Fatalf("cookie file path leaked into logs:\n%s", logs)
	}
	if !strings.Contains(logs, "exit status 1") {
		t.Fatalf("redaction dropped the diagnostic error text:\n%s", logs)
	}
}

func TestDownloadStep_CandidateDownloadFailedLogRedactsCookiePath(t *testing.T) {
	logs := logging.CaptureText(t, slog.LevelDebug)
	step := newTestDownloadStep(&fileWritingSearcher{err: ytdlpCookieErr()})
	ac := &service.AcquisitionContext{Ranked: []ports.AudioCandidate{{URL: "https://example.com/x", Source: "youtube"}}}

	if _, err := step.Execute(context.Background(), ac, service.AfterSelect{}); err == nil {
		t.Fatal("expected download error")
	}

	out := logs.String()
	assertNoCookiePathInText(t, out, "acquisition.candidate_download_failed")
	if !strings.Contains(out, "https://example.com/x") {
		t.Fatalf("candidate URL should stay in the log for triage:\n%s", out)
	}
}

type stubVersionLookup struct {
	mu    sync.Mutex
	facts map[string]ports.RecordingFacts
	err   error
	calls []string
}

func (s *stubVersionLookup) RecordingByMBID(_ context.Context, mbid string) (ports.RecordingFacts, bool, error) {
	s.mu.Lock()
	s.calls = append(s.calls, mbid)
	s.mu.Unlock()
	if s.err != nil {
		return ports.RecordingFacts{}, false, s.err
	}
	facts, found := s.facts[mbid]
	return facts, found, nil
}

func plainTitledLinks(mbids ...string) ports.RecordingMatch {
	recordings := make([]ports.LinkedRecording, 0, len(mbids))
	for _, mbid := range mbids {
		recordings = append(recordings, ports.LinkedRecording{MBID: mbid, Title: "Sunglasses at Night", Artists: []string{"Corey Hart"}})
	}
	return ports.RecordingMatch{AcoustID: "ac-linked", MBIDs: mbids, Score: 0.97, Results: linkedResults(recordings...)}
}

func TestDownloadStep_SoftNeedsTheSameVersionLabels(t *testing.T) {
	plain := ports.RecordingFacts{MBID: "mb-linked", Title: "Sunglasses at Night"}
	acoustic := ports.RecordingFacts{MBID: "mb-linked", Title: "Sunglasses at Night", Disambiguation: "acoustic"}
	remastered := ports.RecordingFacts{MBID: "mb-linked", Title: "Sunglasses at Night", Disambiguation: "remastered"}
	factsLookup := func(facts ports.RecordingFacts) *stubVersionLookup {
		return &stubVersionLookup{facts: map[string]ports.RecordingFacts{facts.MBID: facts}}
	}
	tests := []struct {
		name         string
		lookup       *stubVersionLookup
		title        string
		wantReject   string
		wantVerdict  service.VerdictKind
		wantVerified bool
	}{
		{"an acoustic disambiguation with an acoustic title is another version", factsLookup(acoustic), "Sunglasses at Night (Acoustic)", "contradicted", "", false},
		{"matching labels are soft", factsLookup(plain), "", "", service.VerdictSoft, false},
		{"a remaster disambiguation is the same version", factsLookup(remastered), "", "", service.VerdictSoft, false},
		{"a failed lookup is not soft", &stubVersionLookup{err: errors.New("musicbrainz down")}, "", "", service.VerdictUnknown, false},
		{"an unknown recording is not soft", &stubVersionLookup{}, "", "", service.VerdictUnknown, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			step := newTestDownloadStep(&fileWritingSearcher{writeFile: true},
				withTestIdentifier(&stubIdentifier{match: plainTitledLinks("mb-linked")}), withTestLookup(tt.lookup))
			ac := downloadContext("mb-master")
			for i := range ac.Ranked {
				ac.Ranked[i].Title = tt.title
			}

			_, err := step.Execute(context.Background(), ac, service.AfterSelect{})
			if tt.wantReject != "" {
				if err == nil || len(ac.Rejections) == 0 || !strings.HasPrefix(ac.Rejections[0].Reason, tt.wantReject) {
					t.Fatalf("err = %v, rejections = %+v, want every candidate rejected as %q", err, ac.Rejections, tt.wantReject)
				}
				return
			}
			if tt.wantVerdict == service.VerdictUnknown {
				if !errors.Is(err, service.ErrNoConfidentMatch) {
					t.Fatalf("err = %v, want an unverified match held below the confidence floor", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			defer os.RemoveAll(filepath.Dir(ac.TempPath))
			if ac.Verdict.Kind != tt.wantVerdict {
				t.Errorf("verdict = %q, want %q", ac.Verdict.Kind, tt.wantVerdict)
			}
			if got := ac.Provenance() == downloadprovenance.ProvenanceVerified; got != tt.wantVerified {
				t.Errorf("verified = %v, want %v", got, tt.wantVerified)
			}
		})
	}
}

func TestDownloadStep_ASoftVerdictIsAcceptedButNotIdentityVerified(t *testing.T) {
	tests := []struct {
		name        string
		catalog     float64
		wantProvens downloadprovenance.AcquisitionProvenance
	}{
		{"a corroborated length is corroborated", 180, downloadprovenance.ProvenanceCorroborated},
		{"an uncorroborated length is best effort", 0, downloadprovenance.ProvenanceBestEffort},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lookup := &stubVersionLookup{facts: map[string]ports.RecordingFacts{
				"mb-linked": {MBID: "mb-linked", Title: "Sunglasses at Night"},
			}}
			step := newTestDownloadStep(&fileWritingSearcher{writeFile: true},
				withTestIdentifier(&stubIdentifier{match: plainTitledLinks("mb-linked")}), withTestLookup(lookup),
				withTestProber(&queueProber{durations: []float64{180, 180}}))
			ac := downloadContext("mb-master")
			ac.Track.CatalogDuration = tt.catalog
			ac.Identity.Duration = tt.catalog
			for i := range ac.Ranked {
				ac.Ranked[i].Title = ""
			}

			if _, err := step.Execute(context.Background(), ac, service.AfterSelect{}); err != nil {
				t.Fatalf("Execute: %v", err)
			}
			defer os.RemoveAll(filepath.Dir(ac.TempPath))
			if ac.Verdict.Kind != service.VerdictSoft {
				t.Errorf("verdict = %q, want soft", ac.Verdict.Kind)
			}
			if ac.IdentityVerified {
				t.Error("IdentityVerified = true, want false on a soft verdict")
			}
			if got := ac.Provenance(); got != tt.wantProvens {
				t.Errorf("provenance = %q, want %q", got, tt.wantProvens)
			}
		})
	}
}

func fourLinksOneOfThemAnotherTitle() ports.RecordingMatch {
	match := plainTitledLinks("mb-1", "mb-2", "mb-3", "mb-4")
	match.Results[0].Recordings[3].Title = "Take On Me"
	return match
}

func TestDownloadStep_WidthOne_VersionLookupsAreCappedAndCachedPerJob(t *testing.T) {
	acoustic := func(mbid string) ports.RecordingFacts {
		return ports.RecordingFacts{MBID: mbid, Title: "Sunglasses at Night", Disambiguation: "acoustic"}
	}
	lookup := &stubVersionLookup{facts: map[string]ports.RecordingFacts{
		"mb-1": acoustic("mb-1"), "mb-2": acoustic("mb-2"), "mb-3": acoustic("mb-3"), "mb-4": acoustic("mb-4"),
	}}
	step := newTestDownloadStep(&fileWritingSearcher{writeFile: true},
		withTestIdentifier(&stubIdentifier{match: fourLinksOneOfThemAnotherTitle()}), withTestLookup(lookup), service.WithVerifyWidth(1))
	ac := downloadContext("mb-master")
	for i := range ac.Ranked {
		ac.Ranked[i].Title = "Sunglasses at Night (Acoustic)"
	}

	if _, err := step.Execute(context.Background(), ac, service.AfterSelect{}); err == nil {
		t.Fatal("expected every candidate rejected as another version")
	}
	if len(ac.Rejections) != 2 {
		t.Fatalf("rejections = %+v, want one per candidate", ac.Rejections)
	}
	if want := []string{"mb-1", "mb-2", "mb-3"}; !equalStrings(lookup.calls, want) {
		t.Errorf("lookups = %v, want %v: three per candidate, the second candidate served from the job's cache", lookup.calls, want)
	}
}

func versionLookupsFor(t *testing.T, match ports.RecordingMatch, otherTitled ...int) []string {
	t.Helper()
	for _, i := range otherTitled {
		match.Results[0].Recordings[i].Title = "Take On Me"
		match.Results[0].Recordings[i].Artists = []string{"a-ha"}
	}
	lookup := &stubVersionLookup{facts: map[string]ports.RecordingFacts{}}
	step := newTestDownloadStep(&fileWritingSearcher{writeFile: true},
		withTestIdentifier(&stubIdentifier{match: match}), withTestLookup(lookup), service.WithVerifyWidth(1))
	ac := downloadContext("mb-master")
	ac.Identity.MBIDs = []string{"mb-merged"}
	ac.Ranked = ac.Ranked[:1]
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(ac.TempPath)) })

	_, _ = step.Execute(context.Background(), ac, service.AfterSelect{})
	return lookup.calls
}

func TestDownloadStep_VersionLookupsSkipTheBoundRecordingsAndStartWithTheSameTitle(t *testing.T) {
	calls := versionLookupsFor(t, plainTitledLinks("mb-other", "mb-master", "mb-merged", "mb-dup"), 0)

	if want := []string{"mb-dup", "mb-other"}; !equalStrings(calls, want) {
		t.Errorf("lookups = %v, want %v: the same-title link first, then the other, never the bound or merged recording", calls, want)
	}
}

func TestDownloadStep_AVersionLookupSpendsOneOfTheCandidatesLookupsPerRecording(t *testing.T) {
	calls := versionLookupsFor(t, plainTitledLinks("mb-dup", "mb-o1", "mb-o2", "mb-o3"), 1, 2, 3)

	if want := []string{"mb-dup", "mb-o1", "mb-o2"}; !equalStrings(calls, want) {
		t.Errorf("lookups = %v, want %v: a same-title link is one lookup, not two, so three distinct recordings fit the cap", calls, want)
	}
}

func equalStrings(a, b []string) bool {
	return len(a) == len(b) && strings.Join(a, "\x00") == strings.Join(b, "\x00")
}

func TestDownloadStep_AnOtherVersionWithNoKnownLengthIsNeverStored(t *testing.T) {
	fetcher := newGatedFetcher(nil)
	ac := heldTrackContext(ports.AudioCandidate{
		URL: "edit", Title: "Sunglasses at Night (Radio Edit)", Channel: "Corey Hart - Topic", Duration: 236,
	})
	ac.Track.CatalogDuration = 0
	step := newTestDownloadStep(fetcher, withTestProber(scriptedProber{duration: 236}))

	_, err := step.Execute(context.Background(), ac, service.AfterSelect{})

	if !errors.Is(err, service.ErrNoConfidentMatch) {
		t.Fatalf("err = %v, want ErrNoConfidentMatch", err)
	}
}

func TestDownloadStep_AFingerprintedOtherVersionWithNoKnownLengthIsNeverStored(t *testing.T) {
	fetcher := newGatedFetcher(nil)
	identifier := &urlIdentifier{fetcher: fetcher, matches: map[string]ports.RecordingMatch{"edit": hardMatch()}}
	ac := heldTrackContext(ports.AudioCandidate{
		URL: "edit", Title: "Sunglasses at Night (Radio Edit)", Channel: "Corey Hart - Topic", Duration: 236,
	})
	ac.Track.CatalogDuration = 0
	step := newTestDownloadStep(fetcher, withTestIdentifier(identifier), withTestProber(scriptedProber{duration: 236}))

	_, err := step.Execute(context.Background(), ac, service.AfterSelect{})

	if !errors.Is(err, service.ErrNoConfidentMatch) {
		t.Fatalf("err = %v, want ErrNoConfidentMatch", err)
	}
}

func TestDownloadStep_AFingerprintNamingAnotherSongIsOneSourceSoItIsHeldAndNeverRejected(t *testing.T) {
	identifier := &stubIdentifier{match: differentSongMatch()}
	step := newTestDownloadStep(&fileWritingSearcher{writeFile: true}, withTestIdentifier(identifier))
	ac := downloadContext("mb-master")

	_, err := step.Execute(context.Background(), ac, service.AfterSelect{})

	if !errors.Is(err, service.ErrNoConfidentMatch) || len(ac.Rejections) != 0 || identifier.calls != 2 {
		t.Errorf("err = %v, rejections = %+v, identify calls = %d, want both candidates fingerprinted and held, none rejected", err, ac.Rejections, identifier.calls)
	}
}

func TestDownloadStep_AFingerprintNamingAnotherSongAndATitleWithOtherVersionTagsContradict(t *testing.T) {
	identifier := &stubIdentifier{match: differentSongMatch()}
	step := newTestDownloadStep(&fileWritingSearcher{writeFile: true}, withTestIdentifier(identifier))
	ac := downloadContext("mb-master")
	ac.Ranked = []ports.AudioCandidate{{URL: "https://youtube.com/watch?v=live0000000", Title: "Corey Hart - Sunglasses at Night (Live)"}}

	_, err := step.Execute(context.Background(), ac, service.AfterSelect{})

	if err == nil || len(ac.Rejections) != 1 || ac.Rejections[0].Stage != rejectedcandidates.Fingerprint || !strings.HasPrefix(ac.Rejections[0].Reason, "contradicted") {
		t.Errorf("err = %v, rejections = %+v, want one fingerprint rejection: two sources agree the file is something else", err, ac.Rejections)
	}
}

func TestDownloadStep_TheGradeOfAVerifiedFileIsTheJudgesAndFollowsTheSource(t *testing.T) {
	cases := map[string]struct {
		candidate ports.AudioCandidate
		want      string
	}{
		"an art track": {ports.AudioCandidate{URL: "https://music.youtube.com/watch?v=art00000000", Title: "Sunglasses at Night", Kind: ports.KindArtTrack}, "A"},
		"a fan upload": {ports.AudioCandidate{URL: "https://youtube.com/watch?v=fan00000000", Title: "Sunglasses at Night", Kind: ports.KindUpload, Channel: "Some Fan"}, "B"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			step := newTestDownloadStep(&fileWritingSearcher{writeFile: true}, withTestIdentifier(&stubIdentifier{match: referenceMatch()}))
			ac := downloadContext("mb-master")
			ac.Ranked = []ports.AudioCandidate{tc.candidate}

			if _, err := step.Execute(context.Background(), ac, service.AfterSelect{}); err != nil {
				t.Fatalf("Execute: %v", err)
			}
			defer os.RemoveAll(filepath.Dir(ac.TempPath))

			if got := string(ac.CollectEvidence().Grade); got != tc.want {
				t.Errorf("grade = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDownloadStep_ATrackWithNoCatalogIdentityIsGradedBestAvailable(t *testing.T) {
	step := newTestDownloadStep(&fileWritingSearcher{writeFile: true})
	ac := &service.AcquisitionContext{
		Track:  service.TrackRef{Title: "Leaked Song", Artist: "Corey Hart"},
		Ranked: []ports.AudioCandidate{{URL: "https://youtube.com/watch?v=leak0000000", Title: "Leaked Song", Kind: ports.KindArtTrack}},
	}

	if _, err := step.Execute(context.Background(), ac, service.AfterSelect{}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(ac.TempPath))

	if got := string(ac.CollectEvidence().Grade); got != "best_available" {
		t.Errorf("grade = %q, want best_available: nothing in the catalog to judge the audio against", got)
	}
}

func TestDownloadStep_ASameTitleLinkWhoseVersionWasNeverLookedUpIsNeverLikely(t *testing.T) {
	match := plainTitledLinks("mb-linked")
	step := newTestDownloadStep(&fileWritingSearcher{writeFile: true}, withTestIdentifier(&stubIdentifier{match: match}))
	ac := downloadContext("mb-master")

	_, err := step.Execute(context.Background(), ac, service.AfterSelect{})

	if !errors.Is(err, service.ErrNoConfidentMatch) || ac.IdentityVerified {
		t.Errorf("err = %v, identity verified = %v, want the unverifiable duplicate held as unknown", err, ac.IdentityVerified)
	}
}

func TestDownloadStep_ALinkWithNoMBIDNamesNothingSoItNeverContradicts(t *testing.T) {
	match := ports.RecordingMatch{
		AcoustID: "ac-nameless", Score: 0.97,
		Results: linkedResults(ports.LinkedRecording{Title: "Take On Me", Artists: []string{"a-ha"}}),
	}
	step := newTestDownloadStep(&fileWritingSearcher{writeFile: true}, withTestIdentifier(&stubIdentifier{match: match}))
	ac := downloadContext("mb-master")
	ac.Ranked = []ports.AudioCandidate{{URL: "https://youtube.com/watch?v=live0000000", Title: "Corey Hart - Sunglasses at Night (Live)"}}

	_, err := step.Execute(context.Background(), ac, service.AfterSelect{})

	if len(ac.Rejections) != 0 || !errors.Is(err, service.ErrNoConfidentMatch) {
		t.Errorf("err = %v, rejections = %+v, want the nameless link ignored and the lone title tag held as unknown", err, ac.Rejections)
	}
}

func TestDownloadStep_AZeroFloorIsUsedAsGiven(t *testing.T) {
	fetcher := newGatedFetcher(nil)
	ac := heldTrackContext(ports.AudioCandidate{URL: "reupload", Channel: "chill uploads", Duration: 236})
	step := newTestDownloadStep(fetcher, withTestIdentifier(&urlIdentifier{fetcher: fetcher}), service.WithConfidenceFloor(0))

	if _, err := step.Execute(context.Background(), ac, service.AfterSelect{}); err != nil {
		t.Fatalf("a floor of 0 should store the held candidate, got %v", err)
	}
}

func TestDownloadStep_AnOtherVersionIsStoredOrRejectedByItsProbedLength(t *testing.T) {
	tests := []struct {
		name       string
		probed     float64
		rejected   bool
		wantReason string
	}{
		{"on length is stored", 203, false, ""},
		{"exactly the 6s allowance long is stored", 206, false, ""},
		{"exactly the 6s allowance short is stored", 194, false, ""},
		{"just past the allowance long is rejected as an other version", 206.5, true, fmt.Sprintf(service.RejectOtherVersionOffLengthFmt, 6.5)},
		{"just past the allowance short is rejected as an other version", 193.5, true, fmt.Sprintf(service.RejectOtherVersionOffLengthFmt, -6.5)},
		{"inside the loose window but past the allowance is rejected as an other version", 214, true, fmt.Sprintf(service.RejectOtherVersionOffLengthFmt, 14.0)},
		{"30s off is rejected", 230, true, ""},
		{"no probed length is rejected", 0, true, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ac := heldTrackContext(ports.AudioCandidate{URL: "edit", Title: "Song (Radio Edit)", Channel: "Artist - Topic", Duration: 200})
			ac.Track = service.TrackRef{Title: "Song", Artist: "Artist", CatalogDuration: 200}
			step := newTestDownloadStep(newGatedFetcher(nil), withTestProber(scriptedProber{duration: tt.probed}), service.WithConfidenceFloor(0))

			_, err := step.Execute(context.Background(), ac, service.AfterSelect{})

			if !tt.rejected {
				if err != nil {
					t.Fatalf("Execute = %v, want the other version stored", err)
				}
				return
			}
			if err == nil {
				t.Fatal("Execute = nil, want the other version refused")
			}
			if tt.wantReason == "" {
				return
			}
			if len(ac.Rejections) != 1 || ac.Rejections[0].Reason != tt.wantReason {
				t.Errorf("rejections = %+v, want one with reason %q", ac.Rejections, tt.wantReason)
			}
		})
	}
}

type windowSiblingFetcher struct{ *gatedFetcher }

func (f windowSiblingFetcher) PreviewFetcherFor(ports.AudioCandidate) (ports.PreviewFetcher, bool) {
	return f, true
}

func (f windowSiblingFetcher) FetchPreview(_ context.Context, c ports.AudioCandidate, outDir string, _ int) (string, error) {
	path := filepath.Join(outDir, "preview-"+c.URL+".mp3")
	return path, os.WriteFile(path, []byte("preview-bytes"), 0o644)
}

type cancelledSiblingIdentifier struct {
	fetcher       *gatedFetcher
	rank2Started  chan struct{}
	rank2Starting sync.Once
}

func (i *cancelledSiblingIdentifier) Identify(ctx context.Context, path string, _ float64) (ports.RecordingMatch, error) {
	if strings.Contains(path, "rank2") || filepath.Dir(path) == i.fetcher.dirOf("rank2") {
		i.rank2Starting.Do(func() { close(i.rank2Started) })
		<-ctx.Done()
		return ports.RecordingMatch{}, ctx.Err()
	}
	<-i.rank2Started
	return hardMatch(), nil
}

type cancelledSiblingProber struct{ fetcher *gatedFetcher }

func (p cancelledSiblingProber) ProbeDuration(ctx context.Context, path string) (float64, error) {
	if filepath.Dir(path) == p.fetcher.dirOf("rank2") {
		<-ctx.Done()
		return 0, ctx.Err()
	}
	return 236, nil
}

func (p cancelledSiblingProber) ValidateDecodable(context.Context, string) error { return nil }

func TestDownloadStep_ACancelledWindowSiblingRecordsNoVerifySkip(t *testing.T) {
	fetcher := newGatedFetcher(nil)
	identifier := &cancelledSiblingIdentifier{fetcher: fetcher, rank2Started: make(chan struct{})}
	rec := &gateRecorder{}
	ac := heldTrackContext(
		ports.AudioCandidate{URL: "rank1", Channel: "Corey Hart - Topic", Duration: 236},
		ports.AudioCandidate{URL: "rank2", Channel: "Corey Hart - Topic", Duration: 236},
	)
	step := newTestDownloadStep(windowSiblingFetcher{fetcher},
		withTestIdentifier(identifier), withTestProber(cancelledSiblingProber{fetcher: fetcher}),
		withTestSkips(rec), service.WithVerifyWidth(2))
	logs := logging.CaptureJSON(t, slog.LevelDebug)

	if _, err := step.Execute(context.Background(), ac, service.AfterSelect{}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(ac.TempPath))

	if ac.Selected == nil || ac.Selected.URL != "rank1" {
		t.Fatalf("selected = %+v, want rank1", ac.Selected)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.gates) != 0 {
		t.Errorf("recorded gates = %v, want none for a sibling cancelled by the win", rec.gates)
	}
	for _, event := range []string{"acquisition.probe_failed_accepting", "acquisition.preview_fallback", "acquisition.identify_failed"} {
		if strings.Contains(logs.String(), event) {
			t.Errorf("logged %s for a cancelled sibling", event)
		}
	}
}

func TestDownloadStep_AProbeFailureOnALiveAttemptIsStillASkip(t *testing.T) {
	rec := &gateRecorder{}
	prober := scriptedProber{probeErr: errors.New("ffprobe exploded")}
	step := newTestDownloadStep(&fileWritingSearcher{writeFile: true},
		withTestProber(prober), withTestSkips(rec), service.WithVerifyWidth(1))
	ac := heldTrackContext(ports.AudioCandidate{URL: "only", Duration: 236})

	if _, err := step.Execute(context.Background(), ac, service.AfterSelect{}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(ac.TempPath))

	assertWindowSkips(t, rec.gates, ports.SkipProbeFailed)
}

func TestDownloadStep_ConcurrentCandidatesShareOneVersionLookup(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		lookup := &gatedVersionLookup{started: make(chan struct{}), release: make(chan struct{})}
		step := newTestDownloadStep(&fileWritingSearcher{writeFile: true},
			withTestIdentifier(&stubIdentifier{match: plainTitledLinks("mb-linked")}), withTestLookup(lookup), service.WithVerifyWidth(2))
		ac := downloadContext("mb-master")
		for i := range ac.Ranked {
			ac.Ranked[i].Title = ""
		}
		done := make(chan error, 1)
		go func() {
			_, err := step.Execute(context.Background(), ac, service.AfterSelect{})
			done <- err
		}()
		<-lookup.started
		synctest.Wait()
		close(lookup.release)

		if err := <-done; err != nil {
			t.Fatalf("Execute: %v", err)
		}
		defer os.RemoveAll(filepath.Dir(ac.TempPath))

		if got := lookup.calls.Load(); got != 1 {
			t.Errorf("lookups = %d, want 1: both candidates share one MusicBrainz call", got)
		}
	})
}

type gatedVersionLookup struct {
	started chan struct{}
	release chan struct{}
	calls   atomic.Int32
}

func (g *gatedVersionLookup) RecordingByMBID(ctx context.Context, mbid string) (ports.RecordingFacts, bool, error) {
	if g.calls.Add(1) == 1 {
		close(g.started)
	}
	select {
	case <-g.release:
		return ports.RecordingFacts{MBID: mbid, Title: "Sunglasses at Night"}, true, nil
	case <-ctx.Done():
		return ports.RecordingFacts{}, false, ctx.Err()
	}
}

func TestDownloadStep_ALookupSharedWithACancelledJobStillAnswersTheOtherJob(t *testing.T) {
	logs := logging.CaptureJSON(t, slog.LevelDebug)
	synctest.Test(t, func(t *testing.T) {
		lookup := &gatedVersionLookup{started: make(chan struct{}), release: make(chan struct{})}
		step := newTestDownloadStep(&fileWritingSearcher{writeFile: true},
			withTestIdentifier(&stubIdentifier{match: plainTitledLinks("mb-linked")}), withTestLookup(lookup), service.WithVerifyWidth(1))
		run := func(ctx context.Context) <-chan struct{} {
			done := make(chan struct{})
			ac := downloadContext("mb-master")
			ac.Ranked = ac.Ranked[:1]
			ac.Ranked[0].Title = ""
			go func() {
				defer close(done)
				_, _ = step.Execute(ctx, ac, service.AfterSelect{})
				if ac.TempPath != "" {
					_ = os.RemoveAll(filepath.Dir(ac.TempPath))
				}
			}()
			return done
		}
		leaderCtx, cancelLeader := context.WithCancel(context.Background())
		leader := run(leaderCtx)
		<-lookup.started
		synctest.Wait()
		follower := run(context.Background())
		synctest.Wait()

		cancelLeader()
		synctest.Wait()
		close(lookup.release)
		<-leader
		<-follower

		if got := lookup.calls.Load(); got != 1 {
			t.Errorf("lookups = %d, want 1: the second job shares the first job's call", got)
		}
	})
	if failed := strings.Count(logs.String(), "acquisition.version_lookup_failed"); failed > 1 {
		t.Errorf("%d lookups failed, want at most the cancelled job's own: the other job must not inherit its cancellation:\n%s", failed, logs.String())
	}
}

type panickingVersionLookup struct{}

func (panickingVersionLookup) RecordingByMBID(context.Context, string) (ports.RecordingFacts, bool, error) {
	panic("lookup exploded")
}

func TestDownloadStep_APanickingVersionLookupFailsOnlyThatLookup(t *testing.T) {
	step := newTestDownloadStep(&fileWritingSearcher{writeFile: true},
		withTestIdentifier(&stubIdentifier{match: plainTitledLinks("mb-linked")}), withTestLookup(panickingVersionLookup{}), service.WithVerifyWidth(1))
	ac := downloadContext("mb-master")
	ac.Ranked = ac.Ranked[:1]
	ac.Ranked[0].Title = ""
	defer os.RemoveAll(filepath.Dir(ac.TempPath))

	_, err := step.Execute(context.Background(), ac, service.AfterSelect{})

	if err != nil && strings.Contains(err.Error(), "panic") {
		t.Fatalf("Execute: %v, want the lookup's panic contained as a missing fact", err)
	}
}

func TestDownloadStep_AnEmptyFingerprintIsAVerifySkip(t *testing.T) {
	logs := logging.CaptureJSON(t, slog.LevelDebug)
	rec := &gateRecorder{}
	empty := fmt.Errorf("/tmp/x.mp3: %w", acqdomain.ErrEmptyFingerprint)
	step := newTestDownloadStep(&fileWritingSearcher{writeFile: true},
		withTestIdentifier(&stubIdentifier{err: empty}), withTestSkips(rec))
	ac := downloadContext("mb-master")
	ac.Track.CatalogDuration = 200

	if _, err := step.Execute(context.Background(), ac, service.AfterSelect{}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(ac.TempPath))

	assertWindowSkips(t, rec.gates, "identify_failed")
	findLogRecord(t, logs, "acquisition.identify_failed")
	if strings.Contains(logs.String(), "acquisition.identify_unknown") {
		t.Errorf("empty fingerprint logged as identify_unknown: %s", logs.String())
	}
}

type formatByURLFetcher struct {
	formats map[string]ports.SourceFormat
}

func (f *formatByURLFetcher) Fetch(_ context.Context, c ports.AudioCandidate, outDir string) (ports.FetchedAudio, error) {
	path := filepath.Join(outDir, "track.mp3")
	if err := os.WriteFile(path, []byte("audio-bytes"), 0o644); err != nil {
		return ports.FetchedAudio{}, err
	}
	return ports.FetchedAudio{Path: path, Format: f.formats[c.URL]}, nil
}

func selectedAmongMuxedAndAudioOnly(t *testing.T, width int, muxedKind ports.CandidateKind) string {
	t.Helper()
	const muxedURL, audioOnlyURL = "https://youtube.com/watch?v=muxed0000000", "https://soundcloud.com/a/audio"
	fetcher := &formatByURLFetcher{formats: map[string]ports.SourceFormat{
		muxedURL:     {ID: "18", Kbps: 96},
		audioOnlyURL: {ID: "hls_aac_160k", Kbps: 160},
	}}
	step := mustNewDownloadStep(fetcher, osWorkspace{}, &stubIdentifier{match: referenceMatch()}, ports.NoopRecordingLookup(), downloadtest.SilentAudioProber(), ports.NoopVerifySkipRecorder(), service.Unlimited(), service.WithVerifyWidth(width))
	ac := downloadContext("mb-master")
	ac.Ranked = []ports.AudioCandidate{{URL: muxedURL, Kind: muxedKind}, {URL: audioOnlyURL, Kind: ports.KindUpload}}
	if _, err := step.Execute(context.Background(), ac, service.AfterSelect{}); err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(ac.TempPath))
	return ac.Selected.URL
}

func TestDownloadStep_AMuxedFallbackYieldsToAnAudioOnlyStreamOfTheSameGrade(t *testing.T) {
	for _, width := range []int{1, 2} {
		if got := selectedAmongMuxedAndAudioOnly(t, width, ports.KindUpload); got != "https://soundcloud.com/a/audio" {
			t.Errorf("width %d: selected %s, want the audio-only candidate", width, got)
		}
	}
}

func TestDownloadStep_AMuxedFallbackOfAHigherGradeIsKept(t *testing.T) {
	for _, width := range []int{1, 2} {
		if got := selectedAmongMuxedAndAudioOnly(t, width, ports.KindArtTrack); got != "https://youtube.com/watch?v=muxed0000000" {
			t.Errorf("width %d: selected %s, want the higher-graded muxed candidate", width, got)
		}
	}
}

func TestDownloadStep_WhenEveryCandidateIsMuxedTheFirstAcceptedFileIsStored(t *testing.T) {
	const firstURL, secondURL = "https://youtube.com/watch?v=first0000000", "https://youtube.com/watch?v=second000000"
	for _, width := range []int{1, 2} {
		fetcher := &formatByURLFetcher{formats: map[string]ports.SourceFormat{
			firstURL:  {ID: "18", Kbps: 96},
			secondURL: {ID: "18", Kbps: 96},
		}}
		step := mustNewDownloadStep(fetcher, osWorkspace{}, &stubIdentifier{match: referenceMatch()}, ports.NoopRecordingLookup(), downloadtest.SilentAudioProber(), ports.NoopVerifySkipRecorder(), service.Unlimited(), service.WithVerifyWidth(width))
		ac := downloadContext("mb-master")
		ac.Ranked = []ports.AudioCandidate{{URL: firstURL, Kind: ports.KindUpload}, {URL: secondURL, Kind: ports.KindUpload}}
		if _, err := step.Execute(context.Background(), ac, service.AfterSelect{}); err != nil {
			t.Fatalf("width %d: Execute error: %v", width, err)
		}
		if ac.Selected.URL != firstURL {
			t.Errorf("width %d: selected %s, want the first accepted muxed file", width, ac.Selected.URL)
		}
		if _, err := os.Stat(ac.TempPath); err != nil {
			t.Errorf("width %d: stored file missing: %v", width, err)
		}
		os.RemoveAll(filepath.Dir(ac.TempPath))
	}
}

func TestResolvedIdentity_StandingInTheDarkCatalogLengthConflictReachesTheReaders(t *testing.T) {
	const isrc, mbid = "USAAA2400001", "standing-in-the-dark-live"
	resolver := musicbrainzmatch.NewRecorded(
		[]ports.SearchedRecording{{Title: "Standing In The Dark (Live)", Artist: "Artist", ISRC: isrc, MBID: mbid, Duration: 197}},
		[]ports.ISRCRecording{{MBID: mbid, Duration: 197}}, nil)
	ac := &service.AcquisitionContext{
		Track:   service.TrackRef{Title: "Standing In The Dark (Live)", Artist: "Artist", ISRC: isrc, CatalogDuration: 300},
		Verdict: service.AudioVerdict{Kind: service.VerdictHard, Surviving: []ports.LinkedRecording{{MBID: mbid}}},
	}

	service.ResolveIdentity(context.Background(), resolver, ac)

	if ac.Identity.ReferenceDoubted || ac.Identity.Duration != 197 {
		t.Fatalf("identity = %+v, want the 197s reference undoubted", ac.Identity)
	}
	if got := lengthsOf(ac).Resolved(); got != 197 {
		t.Errorf("resolved length = %v, want 197", got)
	}
	if lengthsOf(ac).Corroborated() {
		t.Error("lengths corroborated = true, want false: catalog 300s and reference 197s still disagree")
	}
	ac.Track.ISRC = ""
	if got, source := provenISRCOf(ac); got != isrc || source != isrcwriteback.Fingerprint {
		t.Errorf("proven ISRC = (%q, %q), want (%q, fingerprint): with the catalog's own ISRC set aside, the surviving recording on the undoubted reference proves it", got, source, isrc)
	}
}

func TestDownloadStep_RollbackLogsAScratchFolderItCouldNotRemove(t *testing.T) {
	logs := logging.CaptureJSON(t, slog.LevelDebug)
	step := mustNewDownloadStep(&fileWritingSearcher{}, failingRemoveWorkspace{}, ports.NoopAudioIdentifier(), ports.NoopRecordingLookup(), downloadtest.SilentAudioProber(), ports.NoopVerifySkipRecorder(), service.Unlimited())

	if err := step.Rollback(context.Background(), &service.AcquisitionContext{TempDir: "/tmp/altune-acquire-x"}); err != nil {
		t.Fatalf("Rollback: %v, want nil", err)
	}

	findLogRecord(t, logs, "temp_cleanup_failed")
}
