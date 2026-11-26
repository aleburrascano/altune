package orphans_test

import (
	"altune/go-api/cmd/api/commands/orphans"
	"altune/go-api/internal/library"
	"altune/go-api/internal/shared/ids"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

type fakeTrackRepo struct {
	tracks  map[library.TrackId]*library.Track
	getN    int
	updates []*library.Track
}

func (f *fakeTrackRepo) GetByID(_ context.Context, id library.TrackId, _ ids.UserId) (*library.Track, error) {
	f.getN++
	return f.tracks[id], nil
}

func (f *fakeTrackRepo) Update(_ context.Context, track *library.Track, _ int) error {
	f.updates = append(f.updates, track)
	return nil
}

var _ library.TrackReadWriter = (*fakeTrackRepo)(nil)

func readyFixture(t *testing.T, ref string) (library.ReadyAudioTrack, *fakeTrackRepo) {
	t.Helper()
	id := library.NewTrackId()
	userId := ids.NewUserId(uuid.New())
	track := &library.Track{ID: id, UserId: userId, DownloadStatus: library.AcquisitionReady, AudioRef: &ref}
	repo := &fakeTrackRepo{tracks: map[library.TrackId]*library.Track{id: track}}
	return library.ReadyAudioTrack{Id: id, UserId: userId, Title: "T", Artist: "A", AudioRef: ref}, repo
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

func TestCheckWithNoTracksMakesNoRepoCalls(t *testing.T) {
	repo := &fakeTrackRepo{}
	store := library.NewFilesystemAudioStore(t.TempDir())

	orphaned, fixed := orphans.Check(context.Background(), nil, store, repo, true)

	if orphaned != 0 || fixed != 0 || repo.getN != 0 || len(repo.updates) != 0 {
		t.Fatalf("got (%d, %d), gets=%d updates=%d; want (0, 0) and no repo calls", orphaned, fixed, repo.getN, len(repo.updates))
	}
}

func TestCheckCountsNothingWhenTheFileExists(t *testing.T) {
	dir := t.TempDir()
	ref := uuid.NewString() + "/song.mp3"
	writeAudio(t, dir, ref)
	track, repo := readyFixture(t, ref)

	orphaned, fixed := orphans.Check(context.Background(), []library.ReadyAudioTrack{track}, library.NewFilesystemAudioStore(dir), repo, true)

	if orphaned != 0 || fixed != 0 || len(repo.updates) != 0 {
		t.Fatalf("got (%d, %d), updates=%d; want (0, 0) and no update", orphaned, fixed, len(repo.updates))
	}
}

func TestCheckReportsMissingFileWithoutFixing(t *testing.T) {
	track, repo := readyFixture(t, uuid.NewString()+"/song.mp3")

	orphaned, fixed := orphans.Check(context.Background(), []library.ReadyAudioTrack{track}, library.NewFilesystemAudioStore(t.TempDir()), repo, false)

	if orphaned != 1 || fixed != 0 || len(repo.updates) != 0 {
		t.Fatalf("got (%d, %d), updates=%d; want (1, 0) and no update", orphaned, fixed, len(repo.updates))
	}
}

func TestCheckMarksMissingFileFailedWhenFixing(t *testing.T) {
	track, repo := readyFixture(t, uuid.NewString()+"/song.mp3")

	orphaned, fixed := orphans.Check(context.Background(), []library.ReadyAudioTrack{track}, library.NewFilesystemAudioStore(t.TempDir()), repo, true)

	if orphaned != 1 || fixed != 1 || len(repo.updates) != 1 {
		t.Fatalf("got (%d, %d), updates=%d; want (1, 1) and one update", orphaned, fixed, len(repo.updates))
	}
	got := repo.updates[0]
	if got.DownloadStatus != library.AcquisitionFailed {
		t.Errorf("status = %v, want failed", got.DownloadStatus)
	}
	if got.FailureReason == nil || *got.FailureReason != "Audio file missing from storage (health-check)" {
		t.Errorf("failure reason = %v, want the health-check reason", got.FailureReason)
	}
}

func TestCheckTreatsASiblingUsersFileAsMissing(t *testing.T) {
	dir := t.TempDir()
	ref := uuid.NewString() + "/song.mp3"
	writeAudio(t, dir, uuid.NewString()+"/song.mp3")
	track, repo := readyFixture(t, ref)

	orphaned, fixed := orphans.Check(context.Background(), []library.ReadyAudioTrack{track}, library.NewFilesystemAudioStore(dir), repo, true)

	if orphaned != 1 || fixed != 1 {
		t.Fatalf("got (%d, %d), want (1, 1)", orphaned, fixed)
	}
}
