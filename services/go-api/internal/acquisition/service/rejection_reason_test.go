package service

import (
	"context"
	"strings"
	"testing"

	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"github.com/google/uuid"
)

// A candidate on the track artist's own channel whose title is padded with
// remaster/quality/year noise scores far below the 60 identity gate. It is the
// Cutting Crew "I've Been in Love Before" failure from issue #31: provenance
// already resembles the track, so the fuzzy title score must not drop it.
func TestRankAndCollect_RescuesLowIdentityOnTheArtistChannel(t *testing.T) {
	track := TrackRef{Title: "I've Been in Love Before", Artist: "Cutting Crew", Duration: 253}
	candidates := []ports.AudioCandidate{{
		Title:   "I've Been In Love Before HQ Stereo Remaster Full Length Version 1986 Special",
		URL:     "yt:master",
		Channel: "Cutting Crew - Topic",
	}}

	ranked, rejected := rankAndCollect(context.Background(), track, candidates)

	if len(ranked) != 1 || ranked[0].URL != "yt:master" {
		t.Fatalf("artist-channel candidate was dropped by the identity gate: ranked=%v", ranked)
	}
	if len(rejected) != 0 {
		t.Errorf("a rescued candidate must not be recorded as rejected: %v", rejected)
	}
}

// The rescue is scoped to the track's own artist: a low-identity candidate on an
// unrelated channel is still gated out, and the reason is captured, not merely
// logged.
func TestRankAndCollect_DropsAndRecordsForeignLowIdentity(t *testing.T) {
	track := TrackRef{Title: "I've Been in Love Before", Artist: "Cutting Crew", Duration: 253}
	candidates := []ports.AudioCandidate{{
		Title:   "Completely Unrelated Cooking Show Full Episode Forty Seven Nonsense",
		URL:     "yt:cooking",
		Channel: "CookingChannel",
	}}

	ranked, rejected := rankAndCollect(context.Background(), track, candidates)

	if len(ranked) != 0 {
		t.Fatalf("an unrelated candidate must stay gated: ranked=%v", ranked)
	}
	if len(rejected) != 1 || rejected[0].Stage != "identity" || rejected[0].URL != "yt:cooking" {
		t.Fatalf("expected one identity rejection for yt:cooking, got %v", rejected)
	}
	if !strings.Contains(rejected[0].Reason, "below threshold") {
		t.Errorf("rejection reason should explain the identity gate: %q", rejected[0].Reason)
	}
}

func TestSummarizeRejections(t *testing.T) {
	tests := []struct {
		name string
		in   []CandidateRejection
		want string
	}{
		{"none", nil, ""},
		{"one", []CandidateRejection{{Stage: "identity"}}, "all 1 candidate rejected (1 identity)"},
		{
			"grouped and sorted",
			[]CandidateRejection{{Stage: "identity"}, {Stage: "duration"}, {Stage: "identity"}},
			"all 3 candidates rejected (1 duration, 2 identity)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := summarizeRejections(tt.in); got != tt.want {
				t.Errorf("summarizeRejections = %q, want %q", got, tt.want)
			}
		})
	}
}

// The whole point of issue #31: when acquisition fails, the persisted
// failure_reason must explain why beyond the generic message, and it survives
// on the track row rather than only in the logs.
func TestExecute_PersistsRejectionSummaryInFailureReason(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	track, err := domain.NewTrack(userId, "I've Been in Love Before", "Cutting Crew", "Broadcast")
	if err != nil {
		t.Fatalf("new track: %v", err)
	}
	repo := newFakeTrackRepository()
	repo.tracks[track.ID.String()+":"+userId.String()] = track

	searcher := &fakeAudioSearcher{searchResults: []ports.AudioCandidate{{
		Title:   "Completely Unrelated Cooking Show Full Episode Forty Seven Nonsense",
		URL:     "yt:cooking",
		Channel: "CookingChannel",
	}}}
	svc := NewAcquireTrackAudioService(repo, fakeRegistry(searcher), newFakeAudioStore())

	_ = svc.Execute(context.Background(), userId, track.ID)

	updated := repo.tracks[track.ID.String()+":"+userId.String()]
	if updated == nil {
		t.Fatal("track missing after acquire")
		return
	}
	if updated.AcquisitionStatus != domain.AcquisitionFailed {
		t.Fatalf("status = %v, want failed", updated.AcquisitionStatus)
	}
	reason := deref(updated.FailureReason)
	if !strings.Contains(reason, "no matching audio found") || !strings.Contains(reason, "1 identity") {
		t.Errorf("failure reason should carry the per-candidate breakdown, got %q", reason)
	}
}
