package service

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

// bytesAudioStore records the bytes behind each ref, so a test can tell the
// original audio apart from a replacement written to the same key.
type bytesAudioStore struct {
	objects   map[string]string
	deleteErr error
}

func (s *bytesAudioStore) Exists(_ context.Context, audioRef string) (bool, error) {
	_, ok := s.objects[audioRef]
	return ok, nil
}

func (s *bytesAudioStore) Store(_ context.Context, sourcePath string, audioRef string) error {
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		return err
	}
	s.objects[audioRef] = string(data)
	return nil
}

func (s *bytesAudioStore) Delete(_ context.Context, audioRef string) error {
	if s.deleteErr != nil {
		return s.deleteErr
	}
	delete(s.objects, audioRef)
	return nil
}

// committingTrackRepo hands out copies so a mutation only becomes visible once
// Update succeeds, the way a database row behaves.
type committingTrackRepo struct {
	rows      map[string]domain.Track
	updateErr error
}

func (r *committingTrackRepo) GetByID(_ context.Context, id domain.TrackId, userId shared.UserId) (*domain.Track, error) {
	row, ok := r.rows[id.String()+":"+userId.String()]
	if !ok {
		return nil, nil
	}
	return &row, nil
}

func (r *committingTrackRepo) Update(_ context.Context, track *domain.Track, _ int) error {
	if r.updateErr != nil {
		return r.updateErr
	}
	r.rows[track.ID.String()+":"+track.UserId.String()] = *track
	return nil
}

func (r *committingTrackRepo) AudioRefInUse(_ context.Context, audioRef string, excludeTrackID domain.TrackId) (bool, error) {
	for _, row := range r.rows {
		if row.ID != excludeTrackID && row.AudioRef != nil && *row.AudioRef == audioRef {
			return true, nil
		}
	}
	return false, nil
}

// committed is the row as last durably written.
func (r *committingTrackRepo) committed(id domain.TrackId, userId shared.UserId) domain.Track {
	return r.rows[id.String()+":"+userId.String()]
}

// newAudioSource offers one fresh candidate and downloads it as "new bytes".
type newAudioSource struct{}

func (newAudioSource) Name() string { return "new-audio" }

func (newAudioSource) Find(_ context.Context, _ ports.FindRequest) ([]ports.AudioCandidate, error) {
	return []ports.AudioCandidate{{
		Title:      "Blinding Lights",
		URL:        "https://www.youtube.com/watch?v=freshAAAAAA",
		Channel:    "The Weeknd - Topic",
		Categories: []string{"Music"},
	}}, nil
}

func (newAudioSource) Fetch(_ context.Context, _ ports.AudioCandidate, outDir string) (string, error) {
	path := filepath.Join(outDir, "audio.mp3")
	return path, os.WriteFile(path, []byte("new bytes"), 0o600)
}

// seedReadyTrack stores a Ready track whose audio lives at the exact key a
// fresh acquisition of the same metadata would build, i.e. the key a replace
// collides with.
func seedReadyTrack(t *testing.T) (*committingTrackRepo, *bytesAudioStore, shared.UserId, domain.TrackId, string) {
	t.Helper()
	userId := shared.NewUserId(uuid.New())
	track, err := domain.NewTrack(userId, "Blinding Lights", "The Weeknd", "After Hours")
	if err != nil {
		t.Fatalf("new track: %v", err)
	}
	originalRef := BuildAudioRef(buildTrackRef(track), "audio.mp3")
	if err := track.MarkReady(originalRef); err != nil {
		t.Fatalf("mark ready: %v", err)
	}
	track.SetAudioSource("https://www.youtube.com/watch?v=currentAAAA")

	repo := &committingTrackRepo{rows: map[string]domain.Track{track.ID.String() + ":" + userId.String(): *track}}
	store := &bytesAudioStore{objects: map[string]string{originalRef: "original bytes"}}
	return repo, store, userId, track.ID, originalRef
}

func TestExecuteReplace_FailedCommitKeepsTheOriginalAudioIntact(t *testing.T) {
	repo, store, userId, trackId, originalRef := seedReadyTrack(t)
	repo.updateErr = errors.New("transient db error")
	svc := NewAcquireTrackAudioService(repo, NewSourceRegistry(newAudioSource{}), store)

	if err := svc.ExecuteReplace(context.Background(), userId, trackId); err == nil {
		t.Fatal("expected the replace to fail when the track update cannot commit")
	}

	if got := store.objects[originalRef]; got != "original bytes" {
		t.Errorf("object at the serving ref = %q, want the original audio untouched", got)
	}
	if len(store.objects) != 1 {
		t.Errorf("objects = %v, want the failed attempt's staged audio rolled back", store.objects)
	}
	row := repo.committed(trackId, userId)
	if row.AcquisitionStatus != domain.AcquisitionReady || row.AudioRef == nil || *row.AudioRef != originalRef {
		t.Errorf("track = %v %v, want it still ready on the original ref", row.AcquisitionStatus, row.AudioRef)
	}
}

func TestExecuteReplace_CommittedSwapPointsAtTheNewAudioAndDropsTheOld(t *testing.T) {
	repo, store, userId, trackId, originalRef := seedReadyTrack(t)
	svc := NewAcquireTrackAudioService(repo, NewSourceRegistry(newAudioSource{}), store)

	if err := svc.ExecuteReplace(context.Background(), userId, trackId); err != nil {
		t.Fatalf("ExecuteReplace: %v", err)
	}

	row := repo.committed(trackId, userId)
	if row.AudioRef == nil || *row.AudioRef == originalRef {
		t.Fatalf("audio_ref = %v, want a new per-attempt ref distinct from %q", row.AudioRef, originalRef)
	}
	if got := store.objects[*row.AudioRef]; got != "new bytes" {
		t.Errorf("object at the committed ref = %q, want the replacement audio", got)
	}
	if _, stillThere := store.objects[originalRef]; stillThere {
		t.Error("the superseded audio must be deleted once the swap has committed")
	}
	if filepath.Ext(*row.AudioRef) != ".mp3" {
		t.Errorf("audio_ref %q lost its extension, which drives the served content type", *row.AudioRef)
	}
}

func TestExecuteReplace_OldAudioDeleteFailureDoesNotUndoTheSwap(t *testing.T) {
	repo, store, userId, trackId, originalRef := seedReadyTrack(t)
	store.deleteErr = errors.New("object storage unavailable")
	svc := NewAcquireTrackAudioService(repo, NewSourceRegistry(newAudioSource{}), store)

	if err := svc.ExecuteReplace(context.Background(), userId, trackId); err != nil {
		t.Fatalf("ExecuteReplace: %v, want the committed swap reported as success", err)
	}

	row := repo.committed(trackId, userId)
	if row.AudioRef == nil || *row.AudioRef == originalRef || store.objects[*row.AudioRef] != "new bytes" {
		t.Errorf("audio_ref = %v, want the committed ref serving the replacement", row.AudioRef)
	}
}

func TestExecute_FreshAcquisitionKeepsTheCanonicalRef(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	track, err := domain.NewTrack(userId, "Blinding Lights", "The Weeknd", "After Hours")
	if err != nil {
		t.Fatalf("new track: %v", err)
	}
	repo := &committingTrackRepo{rows: map[string]domain.Track{track.ID.String() + ":" + userId.String(): *track}}
	store := &bytesAudioStore{objects: map[string]string{}}
	svc := NewAcquireTrackAudioService(repo, NewSourceRegistry(newAudioSource{}), store)

	if err := svc.Execute(context.Background(), userId, track.ID); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	row := repo.committed(track.ID, userId)
	want := BuildAudioRef(buildTrackRef(track), "audio.mp3")
	if row.AudioRef == nil || *row.AudioRef != want {
		t.Errorf("audio_ref = %v, want the canonical %q", row.AudioRef, want)
	}
}
