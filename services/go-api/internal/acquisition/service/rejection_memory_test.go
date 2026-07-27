package service

import (
	"context"
	"errors"
	"testing"

	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"

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
		AudioRef:    "u/a/b/c.mp3",
		ExcludeKeys: []string{"youtube:previousAAA", "youtube:currentAAAA"},
		Selected:    &ports.AudioCandidate{URL: "https://youtube.com/watch?v=freshAAAAAA"},
	}

	if err := NewUpdateTrackStep(repo, userId, track.ID).Execute(context.Background(), ac); err != nil {
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
