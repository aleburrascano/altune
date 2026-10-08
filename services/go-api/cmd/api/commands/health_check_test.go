package commands

import (
	"altune/go-api/internal/catalog/adapters/storage"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

type fakeTrackRepo struct {
	tracks  map[domain.TrackId]*domain.Track
	getN    int
	updates []*domain.Track
}

func (f *fakeTrackRepo) GetByID(_ context.Context, id domain.TrackId, _ shared.UserId) (*domain.Track, error) {
	f.getN++
	return f.tracks[id], nil
}

func (f *fakeTrackRepo) Update(_ context.Context, track *domain.Track, _ int) error {
	f.updates = append(f.updates, track)
	return nil
}

var _ ports.TrackReadWriter = (*fakeTrackRepo)(nil)

func readyFixture(t *testing.T, ref string) (ports.ReadyAudioTrack, *fakeTrackRepo) {
	t.Helper()
	id := domain.NewTrackId()
	userId := shared.NewUserId(uuid.New())
	track := &domain.Track{ID: id, UserId: userId, AcquisitionStatus: domain.AcquisitionReady, AudioRef: &ref}
	repo := &fakeTrackRepo{tracks: map[domain.TrackId]*domain.Track{id: track}}
	return ports.ReadyAudioTrack{Id: id, UserId: userId, Title: "T", Artist: "A", AudioRef: ref}, repo
}

func writeAudio(t *testing.T, dir, ref string) {
	t.Helper()
	path := filepath.Join(dir, ref)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestCheckReadyTracksWithNoTracksMakesNoRepoCalls(t *testing.T) {
	repo := &fakeTrackRepo{}
	store := storage.NewFilesystemAudioStore(t.TempDir())

	orphaned, fixed := checkReadyTracks(context.Background(), nil, store, repo, true)

	if orphaned != 0 || fixed != 0 || repo.getN != 0 || len(repo.updates) != 0 {
		t.Fatalf("got (%d, %d), gets=%d updates=%d; want (0, 0) and no repo calls", orphaned, fixed, repo.getN, len(repo.updates))
	}
}

func TestCheckReadyTracksCountsNothingWhenTheFileExists(t *testing.T) {
	dir := t.TempDir()
	ref := uuid.NewString() + "/song.mp3"
	writeAudio(t, dir, ref)
	track, repo := readyFixture(t, ref)

	orphaned, fixed := checkReadyTracks(context.Background(), []ports.ReadyAudioTrack{track}, storage.NewFilesystemAudioStore(dir), repo, true)

	if orphaned != 0 || fixed != 0 || len(repo.updates) != 0 {
		t.Fatalf("got (%d, %d), updates=%d; want (0, 0) and no update", orphaned, fixed, len(repo.updates))
	}
}

func TestCheckReadyTracksReportsMissingFileWithoutFixing(t *testing.T) {
	track, repo := readyFixture(t, uuid.NewString()+"/song.mp3")

	orphaned, fixed := checkReadyTracks(context.Background(), []ports.ReadyAudioTrack{track}, storage.NewFilesystemAudioStore(t.TempDir()), repo, false)

	if orphaned != 1 || fixed != 0 || len(repo.updates) != 0 {
		t.Fatalf("got (%d, %d), updates=%d; want (1, 0) and no update", orphaned, fixed, len(repo.updates))
	}
}

func TestCheckReadyTracksMarksMissingFileFailedWhenFixing(t *testing.T) {
	track, repo := readyFixture(t, uuid.NewString()+"/song.mp3")

	orphaned, fixed := checkReadyTracks(context.Background(), []ports.ReadyAudioTrack{track}, storage.NewFilesystemAudioStore(t.TempDir()), repo, true)

	if orphaned != 1 || fixed != 1 || len(repo.updates) != 1 {
		t.Fatalf("got (%d, %d), updates=%d; want (1, 1) and one update", orphaned, fixed, len(repo.updates))
	}
	got := repo.updates[0]
	if got.AcquisitionStatus != domain.AcquisitionFailed {
		t.Errorf("status = %v, want failed", got.AcquisitionStatus)
	}
	if got.FailureReason == nil || *got.FailureReason != "Audio file missing from storage (health-check)" {
		t.Errorf("failure reason = %v, want the health-check reason", got.FailureReason)
	}
}

func TestCheckReadyTracksTreatsASiblingUsersFileAsMissing(t *testing.T) {
	dir := t.TempDir()
	ref := uuid.NewString() + "/song.mp3"
	writeAudio(t, dir, uuid.NewString()+"/song.mp3")
	track, repo := readyFixture(t, ref)

	orphaned, fixed := checkReadyTracks(context.Background(), []ports.ReadyAudioTrack{track}, storage.NewFilesystemAudioStore(dir), repo, true)

	if orphaned != 1 || fixed != 1 {
		t.Fatalf("got (%d, %d), want (1, 1)", orphaned, fixed)
	}
}
