package service_test

import (
	"altune/go-api/internal/download/internal/adapters/acquisitiontest"
	"altune/go-api/internal/download/internal/adapters/scratchfiles"
	acqdomain "altune/go-api/internal/download/internal/core/domain"
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/download/service"
	"altune/go-api/internal/shared/downloadfailure"
	"altune/go-api/internal/shared/ids"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/uuid"
)

func TestFailureReason(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"search", &service.StepError{Step: "search", Err: errors.New("no candidates found")}, "no_match_found"},
		{"select", &service.StepError{Step: "select", Err: errors.New("no candidates passed matching gates")}, "no_match_found"},
		{"download", &service.StepError{Step: "download", Err: errors.New("yt-dlp download: exit 1 (stderr: /home/secret/cookies.txt)")}, "download_failed"},
		{"store", &service.StepError{Step: "store", Err: errors.New("store audio: disk full")}, "storage_failed"},
		{"cancelled (pipeline wrap)", fmt.Errorf("pipeline cancelled: %w", context.Canceled), "acquisition_cancelled"},
		{"cancelled (deadline, wrapped in step)", &service.StepError{Step: "download", Err: fmt.Errorf("no candidate produced acceptable audio: %w", context.DeadlineExceeded)}, "acquisition_cancelled"},
		{"unknown step", &service.StepError{Step: "update_track", Err: errors.New("persist track update: boom")}, "acquisition_failed"},
		{"download throttled on every candidate", &service.StepError{Step: "download", Err: fmt.Errorf(
			"no candidate produced acceptable audio: %w",
			&acqdomain.SourceUnavailableError{Source: "ytdlp", Err: errors.New("yt-dlp download: exit status 1 (stderr: HTTP Error 429: Too Many Requests)")},
		)}, "source_unavailable"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := service.FailureCodeOf(tt.err)
			if got != tt.want {
				t.Errorf("FailureCodeOf(%q) = %q, want %q", tt.err, got, tt.want)
			}
		})
	}
}

func TestStepName_IsTheOneItsFailureCodeIsKeyedOn(t *testing.T) {
	tests := []struct {
		step     interface{ Name() service.StepName }
		wantName string
		wantCode downloadfailure.FailureCode
	}{
		{service.NewSearchStep(nil), "search", downloadfailure.FailureNoMatchFound},
		{service.NewSelectStep(), "select", downloadfailure.FailureNoMatchFound},
		{mustNewDownloadStep(&fileWritingSearcher{}, scratchfiles.New(), ports.NoopAudioIdentifier(), ports.NoopRecordingLookup(), ports.NoopAudioProber(), ports.NoopVerifySkipRecorder(), service.Unlimited()), "download", downloadfailure.FailureDownloadFailed},
		{mustNewTagStep(ports.NoopAudioTagger()), "tag", downloadfailure.FailureAcquisitionFailed},
		{mustNewStoreStep(newFakeAudioStore(), scratchfiles.New(), ports.NoopAudioProber(), service.MustDirectDeleteOrphans(&objectStore{objects: map[string]string{}}), ports.NoopAudioRefLookup()), "store", downloadfailure.FailureStorageFailed},
		{service.NewUpdateTrackStep(nil, ids.UserId{}, ids.TrackId{}), "update_track", downloadfailure.FailureAcquisitionFailed},
	}

	for _, tt := range tests {
		t.Run(tt.wantName, func(t *testing.T) {
			name := tt.step.Name()
			code := service.FailureCodeOf(&service.StepError{Step: name, Err: errors.New("boom")})

			if string(name) != tt.wantName {
				t.Errorf("Name() = %q, want %q", name, tt.wantName)
			}
			if code != string(tt.wantCode) {
				t.Errorf("failure code for step %q = %q, want %q", name, code, tt.wantCode)
			}
		})
	}
}

func TestFailureReason_MessageTextAloneIsNotCancellation(t *testing.T) {
	err := errors.New("pipeline cancelled: context canceled")
	if got := service.FailureCodeOf(err); got == "acquisition_cancelled" {
		t.Errorf("FailureCodeOf classified a plain string as cancellation via message text: %q", got)
	}
}

type answeringSource struct {
	name     acqdomain.SourceName
	found    []ports.AudioCandidate
	findErr  error
	fetchErr error
	stall    bool
}

func (s *answeringSource) Name() acqdomain.SourceName { return s.name }

func (s *answeringSource) Find(ctx context.Context, _ ports.FindRequest) ([]ports.AudioCandidate, error) {
	if s.stall {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return s.found, s.findErr
}

func (s *answeringSource) Fetch(context.Context, ports.AudioCandidate, string) (ports.FetchedAudio, error) {
	if s.fetchErr != nil {
		return ports.FetchedAudio{}, s.fetchErr
	}
	return ports.FetchedAudio{}, errors.New("nothing to fetch")
}

func pendingSong() acquisitiontest.LedgerTrack {
	return acquisitiontest.LedgerTrack{TrackSnapshot: ports.TrackSnapshot{
		ID: ids.NewTrackId(), UserId: ids.NewUserId(uuid.New()),
		Title: "Song", Artist: "Artist", Album: "Album", Status: ports.TrackPending,
	}}
}

func acquireService(ledger *acquisitiontest.Ledger, registry *service.SourceRegistry, store ports.AudioWriter) *service.AcquireTrackAudioService {
	return service.MustNewAcquireTrackAudioService(ledger, registry, store, scratchfiles.New(), noVerification(), noStages(deletingFrom(store)))
}

func persistedCode(t *testing.T, ledger *acquisitiontest.Ledger, id ids.TrackId) downloadfailure.FailureCode {
	t.Helper()
	got, ok := ledger.Track(id)
	if !ok || got.Status != ports.TrackFailed {
		t.Fatalf("track = %+v, want it failed", got)
	}
	code, _ := downloadfailure.SplitFailureReason(got.FailureReason)
	return code
}

func TestExecute_SourceThatCouldNotAnswer_IsNotReportedAsNoMatch(t *testing.T) {
	tests := []struct {
		name      string
		searchErr error
		want      downloadfailure.FailureCode
	}{
		{
			name: "every source throttled",
			searchErr: &acqdomain.SourceUnavailableError{
				Source: "ytdlp",
				Err:    errors.New("yt-dlp search: exit status 1 (stderr: HTTP Error 429: Too Many Requests)"),
			},
			want: downloadfailure.FailureSourceUnavailable,
		},
		{
			name:      "the source answered and had nothing",
			searchErr: errors.New("yt-dlp search: 3 lines, 0 parsable"),
			want:      downloadfailure.FailureNoMatchFound,
		},
		{
			name: "the source found nothing",
			want: downloadfailure.FailureNoMatchFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			track := pendingSong()
			ledger := acquisitiontest.NewLedger(track)
			svc := acquireService(ledger, service.NewSourceRegistry(&answeringSource{name: "ytdlp", findErr: tt.searchErr}), &objectStore{objects: map[string]string{}})

			_ = svc.Execute(context.Background(), track.UserId, track.ID)

			if code := persistedCode(t, ledger, track.ID); code != tt.want {
				t.Errorf("persisted failure code = %q, want %q", code, tt.want)
			}
		})
	}
}

func TestExecute_AStoreThatFailsWithTheContextLiveKeepsTheStorageReason(t *testing.T) {
	userID, trackID := ids.NewUserId(uuid.New()), ids.NewTrackId()
	ledger := acquisitiontest.NewLedger(acquisitiontest.LedgerTrack{TrackSnapshot: ports.TrackSnapshot{
		ID: trackID, UserId: userID, Title: "Blinding Lights", Artist: "The Weeknd", Album: "After Hours", Status: ports.TrackPending,
	}})
	store := &objectStore{objects: map[string]string{}, storeErr: errors.New("put object: connection reset")}
	svc := acquireService(ledger, service.NewSourceRegistry(freshAudioSource{}), store)

	err := svc.Execute(context.Background(), userID, trackID)

	if got := service.FailureCodeOf(err); got != string(downloadfailure.FailureStorageFailed) {
		t.Errorf("FailureCodeOf(%v) = %q, want %q", err, got, downloadfailure.FailureStorageFailed)
	}
	if code := persistedCode(t, ledger, trackID); code != downloadfailure.FailureStorageFailed {
		t.Errorf("persisted failure code = %q, want %q", code, downloadfailure.FailureStorageFailed)
	}
}

func TestFailureReason_DropsInternalDetails(t *testing.T) {
	err := &service.StepError{Step: "download", Err: errors.New("yt-dlp download: exit 1 (stderr: /home/secret/cookies.txt)")}
	if reason := service.FailureCodeOf(err); strings.Contains(reason, "cookies") || strings.Contains(reason, "/home") {
		t.Errorf("failure reason leaked internal details: %q", reason)
	}
}

func TestFailureReason_EveryCodeIsKnownToCatalog(t *testing.T) {
	errs := []error{
		errors.New("pipeline cancelled: context canceled"),
		errors.New("unexpected"),
	}
	for _, step := range []service.StepName{"search", "select", "download", "tag", "store", "update_track", "unknown"} {
		errs = append(errs, &service.StepError{Step: step, Err: errors.New("boom")})
	}
	for _, err := range errs {
		code := downloadfailure.FailureCode(service.FailureCodeOf(err))
		if !code.Known() {
			t.Errorf("FailureCodeOf(%q) = %q, not a catalog failure code", err, code)
		}
		reason := downloadfailure.JoinFailureReason(code, "all 1 candidate rejected (1 identity)")
		if got, want := downloadfailure.FailureMessage(&reason), downloadfailure.FailureMessage(new(string(code))); got != want {
			t.Errorf("summary suffix changed message for %q: %q, want %q", code, got, want)
		}
	}
}

func TestFailureReason_WrappedContextErrorIsCancellationForEveryStep(t *testing.T) {
	for _, step := range []service.StepName{"search", "select", "download", "tag", "store", "update_track"} {
		for _, ctxErr := range []error{context.Canceled, context.DeadlineExceeded} {
			err := &service.StepError{Step: step, Err: errors.Join(errors.New("adapter failed"), ctxErr)}
			if got := service.FailureCodeOf(err); got != string(downloadfailure.FailureAcquisitionCancelled) {
				t.Errorf("FailureCodeOf(%s, %v) = %q, want %q", step, ctxErr, got, downloadfailure.FailureAcquisitionCancelled)
			}
		}
	}
}

func TestFailureReason_DownloadStepWrappingNoConfidentMatch(t *testing.T) {
	err := &service.StepError{Step: "download", Err: fmt.Errorf("select: %w", service.ErrNoConfidentMatch)}

	got := service.FailureCodeOf(err)

	if got != "no_confident_match" {
		t.Errorf("FailureCodeOf = %q, want no_confident_match", got)
	}
	if msg := downloadfailure.FailureMessage(&got); msg != "Couldn't find this track" {
		t.Errorf("FailureMessage = %q, want %q", msg, "Couldn't find this track")
	}
	if !downloadfailure.FailureNoConfidentMatch.Known() {
		t.Error("FailureNoConfidentMatch is not Known()")
	}
}

func firstTierOutage() *service.SourceRegistry {
	blocked := &answeringSource{
		name: "ytmsearch",
		found: []ports.AudioCandidate{{
			Title: "Blinding Lights", URL: "https://music.youtube.com/watch?v=artTrackAAA", Kind: ports.KindArtTrack,
		}},
		fetchErr: &acqdomain.SourceUnavailableError{
			Source: "ytmsearch",
			Err:    errors.New("yt-dlp download: HTTP Error 429: Too Many Requests"),
		},
	}
	return service.NewSourceRegistry(blocked).WithFallback(&answeringSource{name: "ytdlp"})
}

func brokenSearchBesideEmptyResults() *service.SourceRegistry {
	broken := &answeringSource{name: "ytdlp", findErr: errors.New("yt-dlp search: output parsed to nothing")}
	return service.NewSourceRegistry(broken, &answeringSource{name: "ytmsearch"})
}

func TestExecute_AFirstTierOutageSurvivesAnEmptyFallback(t *testing.T) {
	track := pendingSong()
	ledger := acquisitiontest.NewLedger(track)
	svc := acquireService(ledger, firstTierOutage(), &objectStore{objects: map[string]string{}})

	execErr := svc.Execute(context.Background(), track.UserId, track.ID)

	if got := service.FailureCodeOf(execErr); got != string(downloadfailure.FailureSourceUnavailable) {
		t.Errorf("failure code = %q, want source_unavailable: the first tier was down, not empty", got)
	}
}

func TestExecute_ABrokenSearchBesideEmptyResultsIsNotNoMatch(t *testing.T) {
	track := pendingSong()
	ledger := acquisitiontest.NewLedger(track)
	svc := acquireService(ledger, brokenSearchBesideEmptyResults(), &objectStore{objects: map[string]string{}})

	execErr := svc.Execute(context.Background(), track.UserId, track.ID)

	if got := service.FailureCodeOf(execErr); got != string(downloadfailure.FailureAcquisitionFailed) {
		t.Errorf("failure code = %q, want acquisition_failed", got)
	}
	if code := persistedCode(t, ledger, track.ID); code != downloadfailure.FailureAcquisitionFailed {
		t.Errorf("persisted failure code = %q, want acquisition_failed", code)
	}
}

func TestScheduler_AFirstTierOutageOnAnEarlyRunIsReleasedForARetry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		track := pendingSong()
		ledger := acquisitiontest.NewLedger(track)
		svc := acquireService(ledger, firstTierOutage(), &objectStore{objects: map[string]string{}})

		outcome := runOneJob(t, svc, track)

		if !outcome.released || outcome.delay != 30*time.Second {
			t.Errorf("outcome = %+v, want the first run released for 30s: the first tier was down, not empty", outcome)
		}
		if got, _ := ledger.Track(track.ID); got.Status != ports.TrackPending {
			t.Errorf("status = %v, want pending while the job waits for its retry", got.Status)
		}
	})
}

func TestScheduler_ABrokenSearchIsSettledWithoutARetry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		track := pendingSong()
		ledger := acquisitiontest.NewLedger(track)
		svc := acquireService(ledger, brokenSearchBesideEmptyResults(), &objectStore{objects: map[string]string{}})

		outcome := runOneJob(t, svc, track)

		if outcome.released {
			t.Errorf("outcome = %+v, want a degraded search settled, not retried", outcome)
		}
		if code := persistedCode(t, ledger, track.ID); code != downloadfailure.FailureAcquisitionFailed {
			t.Errorf("persisted failure code = %q, want acquisition_failed", code)
		}
	})
}

func TestScheduler_AJobThatRunsOutItsOwnTimeIsRetried(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		track := pendingSong()
		ledger := acquisitiontest.NewLedger(track)
		stalled := service.NewSourceRegistry(&answeringSource{name: "ytdlp", stall: true})
		svc := acquireService(ledger, stalled, &objectStore{objects: map[string]string{}})

		outcome := runOneJob(t, svc, track)

		if !outcome.released {
			t.Errorf("outcome = %+v, want the job released: its own timeout is transient", outcome)
		}
		if got, _ := ledger.Track(track.ID); got.Status != ports.TrackPending {
			t.Errorf("status = %v, want pending while the job waits for its retry", got.Status)
		}
	})
}

func TestFailureCode_OwnTimeoutIsReportedAsSourceUnavailable(t *testing.T) {
	err := fmt.Errorf("%w: %w", service.ErrAcquisitionTimedOut, &service.StepError{Step: "download", Err: context.DeadlineExceeded})

	if got := service.FailureCodeOf(err); got != "source_unavailable" {
		t.Errorf("FailureCodeOf = %q, want source_unavailable", got)
	}
}

type retryingAcquirer struct{}

func (retryingAcquirer) Execute(context.Context, ids.UserId, ids.TrackId) error {
	return service.ErrAcquisitionRetryable
}

func (retryingAcquirer) ExecuteReplace(context.Context, ids.UserId, ids.TrackId) error {
	return service.ErrAcquisitionRetryable
}

func (retryingAcquirer) RefuseQueued(context.Context, ids.UserId, ids.TrackId) {}

func backoffAfterRun(t *testing.T, run int) time.Duration {
	t.Helper()
	var delay time.Duration
	synctest.Test(t, func(t *testing.T) {
		outcome := runOneJobAs(t, retryingAcquirer{}, pendingSong(), run)
		if !outcome.released {
			t.Fatalf("run %d: outcome = %+v, want a retryable job released", run, outcome)
		}
		delay = outcome.delay
	})
	return delay
}

func TestRetryBackoff_IsClampedAtTheAttemptCap(t *testing.T) {
	const ceiling = 8 * time.Minute
	for run := 1; run <= 100; run++ {
		got := backoffAfterRun(t, run)
		if got <= 0 || got > ceiling {
			t.Fatalf("backoff after run %d = %v, want within (0, %v]", run, got, ceiling)
		}
	}
	want := []time.Duration{30 * time.Second, 2 * time.Minute, 8 * time.Minute}
	for i, w := range want {
		if got := backoffAfterRun(t, i+1); got != w {
			t.Errorf("backoff after run %d = %v, want %v", i+1, got, w)
		}
	}
}

func TestRetryBackoff_PastTheCapWaitsExactlyAsLongAsAtTheCap(t *testing.T) {
	for _, run := range []int{4, 15, 16, 17, 33, 64, 65, 100, 1 << 20, int(^uint(0) >> 1)} {
		if got := backoffAfterRun(t, run); got != 8*time.Minute {
			t.Errorf("backoff after run %d = %v, want 8m0s", run, got)
		}
	}
}

func TestRetryBackoff_ANonPositiveRunWaitsThePositiveFirstDelay(t *testing.T) {
	for _, run := range []int{0, -1, -64, -int(^uint(0)>>1) - 1} {
		if got := backoffAfterRun(t, run); got != 30*time.Second {
			t.Errorf("backoff after run %d = %v, want 30s", run, got)
		}
	}
}
