package service

import (
	"context"
	"testing"

	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"

	"github.com/google/uuid"
)

func readyTrackWithSource(t *testing.T, repo *fakeTrackRepository, userId shared.UserId, ref, source string) *domain.Track {
	t.Helper()
	track, err := domain.NewTrack(userId, "Blinding Lights", "The Weeknd", "After Hours")
	if err != nil {
		t.Fatalf("new track: %v", err)
	}
	if err := track.MarkReady(ref); err != nil {
		t.Fatalf("mark ready: %v", err)
	}
	track.SetAudioSource(source)
	repo.tracks[track.ID.String()+":"+userId.String()] = track
	return track
}

func TestExecuteReplace_ExcludesTheStoredSource(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	repo := newFakeTrackRepository()
	track := readyTrackWithSource(t, repo, userId, "u/a/b/c.mp3", "https://youtube.com/watch?v=wrong")

	store := newFakeAudioStore()
	store.stored["u/a/b/c.mp3"] = true
	searcher := &fakeAudioSearcher{}
	svc := NewAcquireTrackAudioService(repo, fakeRegistry(searcher), store)

	_ = svc.ExecuteReplace(context.Background(), userId, track.ID)

	if !searcher.searchCalled {
		t.Error("replace must run the search pipeline rather than skipping a ready track")
	}
}

func TestExecuteReplace_FailureLeavesTheTrackReady(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	repo := newFakeTrackRepository()
	track := readyTrackWithSource(t, repo, userId, "u/a/b/c.mp3", "https://youtube.com/watch?v=wrong")

	store := newFakeAudioStore()
	store.stored["u/a/b/c.mp3"] = true
	svc := NewAcquireTrackAudioService(repo, fakeRegistry(&fakeAudioSearcher{}), store)

	if err := svc.ExecuteReplace(context.Background(), userId, track.ID); err == nil {
		t.Fatal("expected the replace to fail with no candidates")
	}

	updated := repo.tracks[track.ID.String()+":"+userId.String()]
	if updated.AcquisitionStatus != domain.AcquisitionReady {
		t.Errorf("status = %v, want the track left ready", updated.AcquisitionStatus)
	}
	if updated.AudioRef == nil || *updated.AudioRef != "u/a/b/c.mp3" {
		t.Errorf("audio_ref = %v, want the original preserved", updated.AudioRef)
	}
	if !store.stored["u/a/b/c.mp3"] {
		t.Error("a failed replace must never delete the audio the user still has")
	}
}

func TestExecute_NonReplaceStillSkipsAReadyTrack(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	repo := newFakeTrackRepository()
	track := readyTrackWithSource(t, repo, userId, "u/a/b/c.mp3", "")

	store := newFakeAudioStore()
	store.stored["u/a/b/c.mp3"] = true
	searcher := &fakeAudioSearcher{}
	svc := NewAcquireTrackAudioService(repo, fakeRegistry(searcher), store)

	_ = svc.Execute(context.Background(), userId, track.ID)

	if searcher.searchCalled {
		t.Error("ordinary acquire must still no-op on a ready track whose audio exists")
	}
}

func TestStoreRollback_KeepsThePreservedRef(t *testing.T) {
	store := newFakeAudioStore()
	store.stored["u/a/b/c.mp3"] = true
	step := NewStoreStep(store)

	ac := &AcquisitionContext{AudioRef: "u/a/b/c.mp3", PreservedRef: "u/a/b/c.mp3"}
	if err := step.Rollback(context.Background(), ac); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if !store.stored["u/a/b/c.mp3"] {
		t.Error("rollback deleted audio the pipeline did not create")
	}
}

func TestStoreRollback_DeletesAFreshlyWrittenRef(t *testing.T) {
	store := newFakeAudioStore()
	store.stored["u/a/b/new.mp3"] = true
	step := NewStoreStep(store)

	ac := &AcquisitionContext{AudioRef: "u/a/b/new.mp3", PreservedRef: "u/a/b/old.mp3"}
	if err := step.Rollback(context.Background(), ac); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if store.stored["u/a/b/new.mp3"] {
		t.Error("rollback must delete an object this run created")
	}
}

func TestReacquireAdmission(t *testing.T) {
	userId := shared.NewUserId(uuid.New())

	pending, _ := domain.NewTrack(userId, "T", "A", "B")
	if err := NewReacquireAdmission().Admit(pending); err != ErrReacquireNotReady {
		t.Errorf("pending track: err = %v, want ErrReacquireNotReady", err)
	}

	failed, _ := domain.NewTrack(userId, "T", "A", "B")
	_ = failed.MarkFailed("boom")
	if err := NewReacquireAdmission().Admit(failed); err != ErrReacquireNotReady {
		t.Errorf("failed track: err = %v, want ErrReacquireNotReady", err)
	}

	ready, _ := domain.NewTrack(userId, "T", "A", "B")
	_ = ready.MarkReady("u/a/b/c.mp3")
	admission := NewReacquireAdmission()
	if err := admission.Admit(ready); err != nil {
		t.Fatalf("ready track: err = %v, want admitted", err)
	}
	if err := admission.Admit(ready); err != ErrRetryCooldown {
		t.Errorf("second call: err = %v, want ErrRetryCooldown", err)
	}
}

func TestRetryAdmission_StillFailedOnlyWithCooldown(t *testing.T) {
	userId := shared.NewUserId(uuid.New())

	ready, _ := domain.NewTrack(userId, "T", "A", "B")
	_ = ready.MarkReady("u/a/b/c.mp3")
	if err := NewRetryAdmission().Admit(ready); err != ErrRetryNotFailed {
		t.Errorf("ready track: err = %v, want ErrRetryNotFailed", err)
	}

	failed, _ := domain.NewTrack(userId, "T", "A", "B")
	_ = failed.MarkFailed("boom")
	admission := NewRetryAdmission()
	if err := admission.Admit(failed); err != nil {
		t.Fatalf("failed track: err = %v, want admitted", err)
	}
	if err := admission.Admit(failed); err != ErrRetryCooldown {
		t.Errorf("second call: err = %v, want ErrRetryCooldown", err)
	}
}

func TestExecuteReplace_LegacyTrackSkipsTopRankedInsteadOfExcluding(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	repo := newFakeTrackRepository()
	track := readyTrackWithSource(t, repo, userId, "u/a/b/c.mp3", "")

	store := newFakeAudioStore()
	store.stored["u/a/b/c.mp3"] = true
	svc := NewAcquireTrackAudioService(repo, fakeRegistry(&fakeAudioSearcher{}), store)

	_ = svc.ExecuteReplace(context.Background(), userId, track.ID)

	updated := repo.tracks[track.ID.String()+":"+userId.String()]
	if updated.AcquisitionStatus != domain.AcquisitionReady {
		t.Errorf("status = %v, want the track left ready", updated.AcquisitionStatus)
	}
}

func TestSelectStep_SkipTopRankedDropsTheLeader(t *testing.T) {
	ac := &AcquisitionContext{
		Track:         TrackRef{Title: "Blinding Lights", Artist: "The Weeknd", Duration: 200},
		SkipTopRanked: true,
		Candidates: []ports.AudioCandidate{
			{Title: "Blinding Lights", URL: "leader", Channel: "The Weeknd - Topic", Duration: 200},
			{Title: "The Weeknd - Blinding Lights", URL: "runner-up", Channel: "Someone", Duration: 201},
		},
	}

	if err := NewSelectStep().Execute(context.Background(), ac); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if ac.Selected == nil || ac.Selected.URL != "runner-up" {
		t.Fatalf("selected = %+v, want the runner-up", ac.Selected)
	}
}

func TestSelectStep_SkipTopRankedWithOneCandidateFails(t *testing.T) {
	ac := &AcquisitionContext{
		Track:         TrackRef{Title: "Blinding Lights", Artist: "The Weeknd", Duration: 200},
		SkipTopRanked: true,
		Candidates: []ports.AudioCandidate{
			{Title: "Blinding Lights", URL: "only", Channel: "The Weeknd - Topic", Duration: 200},
		},
	}

	if err := NewSelectStep().Execute(context.Background(), ac); err == nil {
		t.Fatal("expected failure rather than re-storing the only candidate")
	}
}

func TestSelectStep_SkipTopRankedOffByDefault(t *testing.T) {
	ac := &AcquisitionContext{
		Track: TrackRef{Title: "Blinding Lights", Artist: "The Weeknd", Duration: 200},
		Candidates: []ports.AudioCandidate{
			{Title: "Blinding Lights", URL: "leader", Channel: "The Weeknd - Topic", Duration: 200},
		},
	}

	if err := NewSelectStep().Execute(context.Background(), ac); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if ac.Selected == nil || ac.Selected.URL != "leader" {
		t.Fatalf("an ordinary acquire must keep the leader, got %+v", ac.Selected)
	}
}
