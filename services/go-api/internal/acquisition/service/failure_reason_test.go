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
		// Issue #963: classify on the wrapped context error, not on a message
		// prefix. runStage wraps ctx.Err() with %w; errors.Is must reach it.
		{"cancelled (pipeline wrap)", fmt.Errorf("pipeline cancelled: %w", context.Canceled), "acquisition_cancelled"},
		{"cancelled (deadline, wrapped in step)", &StepError{Step: "download", Err: fmt.Errorf("no candidate produced acceptable audio: %w", context.DeadlineExceeded)}, "acquisition_cancelled"},
		{"unknown step", &StepError{Step: "update_track", Err: errors.New("persist track update: boom")}, "acquisition_failed"},
		// Issue #1983: the last candidate's throttle is what the download step
		// wraps, and the step name alone would call all eight a failed download.
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

// Issue #963: classification must not depend on message text. A message that
// merely reads "pipeline cancelled" but wraps no context error is a genuine
// failure, not a cancellation — a reworded prefix must never flip the branch.
func TestFailureReason_MessageTextAloneIsNotCancellation(t *testing.T) {
	err := errors.New("pipeline cancelled: context canceled")
	if got := failureReason(err); got == "acquisition_cancelled" {
		t.Errorf("failureReason classified a plain string as cancellation via message text: %q", got)
	}
}

// Issue #1983: a source that was throttled, unreachable, or never installed is
// evidence about the source and none about the track. Classified by the step it
// died in, a rate-limited search persists as no_match_found and tells the user
// a track that exists does not.
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
			code, _, _ := strings.Cut(*updated.FailureReason, domain.FailureDetailSeparator)
			if code != string(tt.want) {
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
