package service

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestFailureReason(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"search", &StepError{Step: "search", Err: errors.New("no candidates found")}, "no_match_found"},
		{"select", &StepError{Step: "select", Err: errors.New("no candidates passed matching gates")}, "no_match_found"},
		{"download", &StepError{Step: "download", Err: errors.New("yt-dlp download: exit 1 (stderr: /home/secret/cookies.txt)")}, "download_failed"},
		{"store", &StepError{Step: "store", Err: errors.New("store audio: disk full")}, "storage_failed"},
		{"cancelled (pipeline wrap)", fmt.Errorf("pipeline cancelled: %w", context.Canceled), "acquisition_cancelled"},
		{"cancelled (deadline, wrapped in step)", &StepError{Step: "download", Err: fmt.Errorf("no candidate produced acceptable audio: %w", context.DeadlineExceeded)}, "acquisition_cancelled"},
		{"unknown step", &StepError{Step: "update_track", Err: errors.New("persist track update: boom")}, "acquisition_failed"},
		{"download throttled on every candidate", &StepError{Step: "download", Err: fmt.Errorf(
			"no candidate produced acceptable audio: %w",
			&ports.SourceUnavailableError{Source: "ytdlp", Err: errors.New("yt-dlp download: exit status 1 (stderr: HTTP Error 429: Too Many Requests)")},
		)}, "source_unavailable"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := failureReason(tt.err)
			if got != tt.want {
				t.Errorf("failureReason(%q) = %q, want %q", tt.err, got, tt.want)
			}
		})
	}
}

func TestStepName_IsTheOneItsFailureCodeIsKeyedOn(t *testing.T) {
	tests := []struct {
		step     undoable
		wantName string
		wantCode domain.FailureCode
	}{
		{NewSearchStep(nil), "search", domain.FailureNoMatchFound},
		{NewSelectStep(), "select", domain.FailureNoMatchFound},
		{NewDownloadStep(nil), "download", domain.FailureDownloadFailed},
		{NewTagStep(nil), "tag", domain.FailureAcquisitionFailed},
		{NewStoreStep(nil), "store", domain.FailureStorageFailed},
		{NewUpdateTrackStep(nil, shared.UserId{}, domain.TrackId{}), "update_track", domain.FailureAcquisitionFailed},
	}

	for _, tt := range tests {
		t.Run(tt.wantName, func(t *testing.T) {
			name := tt.step.Name()
			code := failureCode(&StepError{Step: name, Err: errors.New("boom")})

			if string(name) != tt.wantName {
				t.Errorf("Name() = %q, want %q", name, tt.wantName)
			}
			if code != tt.wantCode {
				t.Errorf("failureCode for step %q = %q, want %q", name, code, tt.wantCode)
			}
		})
	}
}

func TestFailureReason_MessageTextAloneIsNotCancellation(t *testing.T) {
	err := errors.New("pipeline cancelled: context canceled")
	if got := failureReason(err); got == "acquisition_cancelled" {
		t.Errorf("failureReason classified a plain string as cancellation via message text: %q", got)
	}
}

func TestExecute_SourceThatCouldNotAnswer_IsNotReportedAsNoMatch(t *testing.T) {
	tests := []struct {
		name      string
		searchErr error
		want      domain.FailureCode
	}{
		{
			name: "every source throttled",
			searchErr: &ports.SourceUnavailableError{
				Source: "ytdlp",
				Err:    errors.New("yt-dlp search: exit status 1 (stderr: HTTP Error 429: Too Many Requests)"),
			},
			want: domain.FailureSourceUnavailable,
		},
		{
			name:      "the source answered and had nothing",
			searchErr: errors.New("yt-dlp search: 3 lines, 0 parsable"),
			want:      domain.FailureNoMatchFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			userId := shared.NewUserId(uuid.New())
			track, err := domain.NewTrack(userId, "Song", "Artist", "Album")
			if err != nil {
				t.Fatalf("new track: %v", err)
			}
			repo := newFakeTrackRepository()
			repo.tracks[track.ID.String()+":"+userId.String()] = track
			finder := &fakeAudioSearcher{searchErr: tt.searchErr}
			svc := NewAcquireTrackAudioService(repo, fakeRegistry(finder), newFakeAudioStore())

			_ = svc.Execute(context.Background(), userId, track.ID)

			updated := repo.tracks[track.ID.String()+":"+userId.String()]
			if updated == nil || updated.FailureReason == nil {
				t.Fatalf("track was not failed: %+v", updated)
			}
			code, _ := domain.SplitFailureReason(*updated.FailureReason)
			if code != tt.want {
				t.Errorf("persisted failure code = %q, want %q", code, tt.want)
			}
		})
	}
}

func TestFailureReason_DropsInternalDetails(t *testing.T) {
	err := &StepError{Step: "download", Err: errors.New("yt-dlp download: exit 1 (stderr: /home/secret/cookies.txt)")}
	if reason := failureReason(err); strings.Contains(reason, "cookies") || strings.Contains(reason, "/home") {
		t.Errorf("failure reason leaked internal details: %q", reason)
	}
}

func TestFailureReason_EveryCodeIsKnownToCatalog(t *testing.T) {
	errs := []error{
		errors.New("pipeline cancelled: context canceled"),
		errors.New("unexpected"),
	}
	for _, step := range []StepName{"search", "select", "download", "tag", "store", "update_track", "unknown"} {
		errs = append(errs, &StepError{Step: step, Err: errors.New("boom")})
	}
	for _, err := range errs {
		code := failureCode(err)
		if !code.Known() {
			t.Errorf("failureReason(%q) = %q, not a catalog failure code", err, code)
		}
		reason := domain.JoinFailureReason(code, "all 1 candidate rejected (1 identity)")
		if got, want := domain.FailureMessage(&reason), domain.FailureMessage(new(string(code))); got != want {
			t.Errorf("summary suffix changed message for %q: %q, want %q", code, got, want)
		}
	}
}

func TestSearchAndStoreSteps_GenuineFailure_KeepsStepReason(t *testing.T) {
	ctx := context.Background()

	_, searchErr := NewSearchStep(&cancellingFinder{cancel: func() {}}).
		Execute(ctx, &AcquisitionContext{Track: TrackRef{Title: "Song", Artist: "Artist"}}, pipelineStart{})
	if got := failureReason(&StepError{Step: "search", Err: searchErr}); got != string(domain.FailureNoMatchFound) {
		t.Errorf("search failureReason = %q, want %q", got, domain.FailureNoMatchFound)
	}

	ac := &AcquisitionContext{Track: TrackRef{UserID: "u1", Title: "Song", Artist: "Artist"}, TempPath: "/tmp/x/track.mp3"}
	_, storeErr := NewStoreStep(&cancellingWriter{cancel: func() {}}).Execute(ctx, ac, afterTag{})
	if got := failureReason(&StepError{Step: "store", Err: storeErr}); got != string(domain.FailureStorageFailed) {
		t.Errorf("store failureReason = %q, want %q", got, domain.FailureStorageFailed)
	}
}

func TestFailureReason_WrappedContextErrorIsCancellationForEveryStep(t *testing.T) {
	for _, step := range []StepName{"search", "select", "download", "tag", "store", "update_track"} {
		for _, ctxErr := range []error{context.Canceled, context.DeadlineExceeded} {
			err := &StepError{Step: step, Err: errors.Join(errors.New("adapter failed"), ctxErr)}
			if got := failureReason(err); got != string(domain.FailureAcquisitionCancelled) {
				t.Errorf("failureReason(%s, %v) = %q, want %q", step, ctxErr, got, domain.FailureAcquisitionCancelled)
			}
		}
	}
}

func TestFailureReason_DownloadStepWrappingNoConfidentMatch(t *testing.T) {
	err := &StepError{Step: stepNameDownload, Err: fmt.Errorf("select: %w", ErrNoConfidentMatch)}

	got := failureReason(err)

	if got != "no_confident_match" {
		t.Errorf("failureReason = %q, want no_confident_match", got)
	}
	if msg := domain.FailureMessage(&got); msg != "Couldn't find this track" {
		t.Errorf("FailureMessage = %q, want %q", msg, "Couldn't find this track")
	}
	if !domain.FailureNoConfidentMatch.Known() {
		t.Error("FailureNoConfidentMatch is not Known()")
	}
}
