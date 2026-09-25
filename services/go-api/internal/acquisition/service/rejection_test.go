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

func TestExecuteReplace_SeedsExclusionsFromMemoryAndTheCurrentSource(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	repo := newFakeTrackRepository()
	track := readyTrackWithSource(t, repo, userId, "u/a/b/c.mp3",
		"https://music.youtube.com/watch?v=currentAAAA")
	track.RejectAudioSource("youtube:previousAAA")

	store := newFakeAudioStore()
	store.stored["u/a/b/c.mp3"] = true

	captured := &capturingSource{}
	svc := NewAcquireTrackAudioService(repo, NewSourceRegistry(captured), store)

	_ = svc.ExecuteReplace(context.Background(), userId, track.ID)

	if !captured.tried("https://www.youtube.com/watch?v=freshAAAAAA") {
		t.Fatalf("the replace never reached an unexcluded candidate; tried %v", captured.fetched)
	}
	if captured.tried("https://www.youtube.com/watch?v=currentAAAA") {
		t.Error("the currently stored source must be excluded under any URL spelling")
	}
	if captured.tried("https://www.youtube.com/watch?v=previousAAA") {
		t.Error("a source rejected by an earlier replace must stay excluded — otherwise re-acquire toggles")
	}
}

func TestUpdateTrackStep_PersistsTheRejectedSources(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	track, err := domain.NewTrack(userId, "Sunglasses at Night", "Corey Hart", "First Offense")
	if err != nil {
		t.Fatalf("new track: %v", err)
	}
	repo := newFakeTrackRepository()
	repo.tracks[track.ID.String()+":"+userId.String()] = track

	ac := &AcquisitionContext{
		AudioRef: "u/a/b/c.mp3",
		Replace:  ReplaceState{ExcludeKeys: []string{"youtube:previousAAA", "youtube:currentAAAA"}},
		Selected: &ports.AudioCandidate{URL: "https://youtube.com/watch?v=freshAAAAAA"},
	}

	if _, err := NewUpdateTrackStep(repo, userId, track.ID).Execute(context.Background(), ac, afterStore{}); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	updated := repo.tracks[track.ID.String()+":"+userId.String()]
	if len(updated.RejectedSourceKeys) != 2 {
		t.Fatalf("rejected keys = %v, want both carried forward", updated.RejectedSourceKeys)
	}
	if updated.AudioSourceURL == nil || *updated.AudioSourceURL != "https://youtube.com/watch?v=freshAAAAAA" {
		t.Errorf("audio source = %v, want the newly stored candidate", updated.AudioSourceURL)
	}
}

func TestRejectAudioSource_IsIdempotentAndBounded(t *testing.T) {
	track := &domain.Track{}

	track.RejectAudioSource("youtube:aaa")
	track.RejectAudioSource("youtube:aaa")
	if len(track.RejectedSourceKeys) != 1 {
		t.Errorf("keys = %v, want no duplicate", track.RejectedSourceKeys)
	}

	track.RejectAudioSource("")
	if len(track.RejectedSourceKeys) != 1 {
		t.Errorf("keys = %v, want an empty key ignored", track.RejectedSourceKeys)
	}

	for i := 0; i < 60; i++ {
		track.RejectAudioSource(string(rune('a'+i%26)) + string(rune('0'+i/26)))
	}
	if len(track.RejectedSourceKeys) > 25 {
		t.Errorf("keys = %d, want the list bounded so a row cannot grow without limit", len(track.RejectedSourceKeys))
	}
}

type capturingSource struct {
	fetched []string
}

func (c *capturingSource) Name() string { return "capturing" }

func (c *capturingSource) Find(_ context.Context, _ ports.FindRequest) ([]ports.AudioCandidate, error) {
	offered := []string{
		"https://www.youtube.com/watch?v=currentAAAA",
		"https://www.youtube.com/watch?v=previousAAA",
		"https://www.youtube.com/watch?v=freshAAAAAA",
	}
	out := make([]ports.AudioCandidate, 0, len(offered))
	for _, url := range offered {
		out = append(out, ports.AudioCandidate{
			Title:      "Blinding Lights",
			URL:        url,
			Channel:    "The Weeknd - Topic",
			Categories: []string{"Music"},
		})
	}
	return out, nil
}

func (c *capturingSource) Fetch(_ context.Context, candidate ports.AudioCandidate, _ string) (string, error) {
	c.fetched = append(c.fetched, candidate.URL)
	return "", errors.New("fetch disabled in this test")
}

func (c *capturingSource) tried(url string) bool {
	for _, got := range c.fetched {
		if got == url {
			return true
		}
	}
	return false
}

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

// RejectionStage values are persisted inside failure_reason and logged, so
// each constant must keep its original literal byte-for-byte.
func TestRejectionStage_LiteralsArePinned(t *testing.T) {
	want := map[RejectionStage]string{
		RejectionIdentity:     "identity",
		RejectionDownload:     "download",
		RejectionDuration:     "duration",
		RejectionUndecodable:  "undecodable",
		RejectionFingerprint:  "fingerprint",
		RejectionNotAttempted: "not_attempted",
	}
	if len(want) != 6 {
		t.Fatalf("rejection stage constants collide: %v", want)
	}
	for stage, literal := range want {
		if string(stage) != literal {
			t.Errorf("RejectionStage %q, want %q", stage, literal)
		}
	}
	rejections := []CandidateRejection{
		{Stage: RejectionFingerprint},
		{Stage: RejectionDownload},
		{Stage: RejectionUndecodable},
		{Stage: RejectionDuration},
		{Stage: RejectionIdentity},
		{Stage: RejectionDownload},
	}
	const summary = "all 6 candidates rejected (2 download, 1 duration, 1 fingerprint, 1 identity, 1 undecodable)"
	if got := summarizeRejections(rejections); got != summary {
		t.Errorf("summarizeRejections = %q, want %q", got, summary)
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
	if !strings.Contains(reason, "no_match_found") || !strings.Contains(reason, "1 identity") {
		t.Errorf("failure reason should carry the per-candidate breakdown, got %q", reason)
	}
}

// Issue #987: the download loop stops at maxDownloadAttempts even when more
// candidates are ranked. The persisted summary must say so, or an operator
// cannot tell an exhaustive failure from a capped one.
func TestExecute_AttemptCapLeavesUntriedCandidatesInTheSummary(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	track, err := domain.NewTrack(userId, "I've Been in Love Before", "Cutting Crew", "Broadcast")
	if err != nil {
		t.Fatalf("new track: %v", err)
	}
	repo := newFakeTrackRepository()
	repo.tracks[track.ID.String()+":"+userId.String()] = track

	const ranked = maxDownloadAttempts + 3
	candidates := make([]ports.AudioCandidate, 0, ranked)
	for i := range ranked {
		candidates = append(candidates, ports.AudioCandidate{
			Title:   "Cutting Crew - I've Been in Love Before",
			URL:     fmt.Sprintf("yt:cutting-crew-%02d", i),
			Channel: "Cutting Crew",
		})
	}
	searcher := &fakeAudioSearcher{searchResults: candidates, downloadErr: errors.New("boom")}
	svc := NewAcquireTrackAudioService(repo, fakeRegistry(searcher), newFakeAudioStore())

	_ = svc.Execute(context.Background(), userId, track.ID)

	if len(searcher.downloadURLs) != maxDownloadAttempts {
		t.Fatalf("download attempts = %d, want the cap %d", len(searcher.downloadURLs), maxDownloadAttempts)
	}
	updated := repo.tracks[track.ID.String()+":"+userId.String()]
	if updated == nil {
		t.Fatal("track missing after acquire")
		return
	}
	want := fmt.Sprintf("all %d candidates rejected (%d download, 3 not_attempted)", ranked, maxDownloadAttempts)
	if reason := deref(updated.FailureReason); !strings.Contains(reason, want) {
		t.Errorf("failure reason = %q, want it to contain %q", reason, want)
	}
}
