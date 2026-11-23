package service

import (
	"altune/go-api/internal/acquisition/internal/adapters/catalogbridge"
	acqdomain "altune/go-api/internal/acquisition/internal/core/domain"
	"altune/go-api/internal/acquisition/internal/core/ports"
	"altune/go-api/internal/acquisition/internal/core/rejectionlog"
	"altune/go-api/internal/catalog/domain"
	shared "altune/go-api/internal/shared/kernel"
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
	svc := newTestAcquireService(catalogbridge.NewTrackLedger(repo), NewSourceRegistry(captured), store)

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

	if _, err := NewUpdateTrackStep(catalogbridge.NewTrackLedger(repo), userId, track.ID).Execute(context.Background(), ac, afterStore{}); err != nil {
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

type capturingSource struct {
	fetched []string
}

func (c *capturingSource) Name() acqdomain.SourceName { return "capturing" }

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

func (c *capturingSource) Fetch(_ context.Context, candidate ports.AudioCandidate, _ string) (ports.FetchedAudio, error) {
	c.fetched = append(c.fetched, candidate.URL)
	return fetchedAt("", errors.New("fetch disabled in this test"))
}

func (c *capturingSource) tried(url string) bool {
	for _, got := range c.fetched {
		if got == url {
			return true
		}
	}
	return false
}

func TestRankAndCollect_RescuesLowIdentityOnTheArtistChannel(t *testing.T) {
	track := TrackRef{Title: "I've Been in Love Before", Artist: "Cutting Crew", CatalogDuration: 253}
	candidates := []ports.AudioCandidate{{
		Title:   "I've Been In Love Before HQ Stereo Remaster Full Length Version 1986 Special",
		URL:     "yt:master",
		Channel: "Cutting Crew - Topic",
	}}

	ranked, rejected := rankAndCollect(context.Background(), track, track.CatalogDuration, candidates)

	if len(ranked) != 1 || ranked[0].URL != "yt:master" {
		t.Fatalf("artist-channel candidate was dropped by the identity gate: ranked=%v", ranked)
	}
	if len(rejected) != 0 {
		t.Errorf("a rescued candidate must not be recorded as rejected: %v", rejected)
	}
}

func TestRankAndCollect_DropsAndRecordsForeignLowIdentity(t *testing.T) {
	track := TrackRef{Title: "I've Been in Love Before", Artist: "Cutting Crew", CatalogDuration: 253}
	candidates := []ports.AudioCandidate{{
		Title:   "Completely Unrelated Cooking Show Full Episode Forty Seven Nonsense",
		URL:     "yt:cooking",
		Channel: "CookingChannel",
	}}

	ranked, rejected := rankAndCollect(context.Background(), track, track.CatalogDuration, candidates)

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
	svc := newTestAcquireService(catalogbridge.NewTrackLedger(repo), fakeRegistry(searcher), newFakeAudioStore())

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

func TestExecute_AttemptCapReportsUntriedCandidatesApart(t *testing.T) {
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
	svc := newTestAcquireService(catalogbridge.NewTrackLedger(repo), fakeRegistry(searcher), newFakeAudioStore())

	_ = svc.Execute(context.Background(), userId, track.ID)

	if len(searcher.downloadURLs) != maxDownloadAttempts {
		t.Fatalf("download attempts = %d, want the cap %d", len(searcher.downloadURLs), maxDownloadAttempts)
	}
	updated := repo.tracks[track.ID.String()+":"+userId.String()]
	if updated == nil {
		t.Fatal("track missing after acquire")
		return
	}
	want := fmt.Sprintf("all %d candidates rejected (%d download), 3 not tried", maxDownloadAttempts, maxDownloadAttempts)
	if reason := deref(updated.FailureReason); !strings.Contains(reason, want) {
		t.Errorf("failure reason = %q, want it to contain %q", reason, want)
	}
}

func TestRejectionBasis_CarriesTheRunsExpectedLengthAndReferenceMBID(t *testing.T) {
	ac := &AcquisitionContext{
		Track:    TrackRef{ID: "t1", CatalogDuration: 322},
		Identity: ports.RecordingIdentity{MBID: "mbid-1"},
	}

	want := ports.RejectionBasis{ExpectedSeconds: 322, ReferenceMBID: "mbid-1"}
	if got := ac.rejectionBasis(); got != want {
		t.Errorf("rejectionBasis = %+v, want %+v", got, want)
	}
}

func TestRecordUnmeasuredRejection_IsKeptForTheSummary(t *testing.T) {
	ac := &AcquisitionContext{Track: TrackRef{ID: "t1"}}
	ac.recordUnmeasuredRejection("https://youtube.com/watch?v=a", "", "yt", rejectionlog.Duration, "search duration 30s vs expected 200s")
	ac.recordRejection("https://youtube.com/watch?v=b", "", "yt", rejectionlog.Duration, "duration 30s vs expected 200s")

	if len(ac.Rejections) != 2 || !ac.Rejections[0].Unmeasured || ac.Rejections[1].Unmeasured {
		t.Errorf("run rejections = %+v, want both kept and only the first unmeasured", ac.Rejections)
	}
}
