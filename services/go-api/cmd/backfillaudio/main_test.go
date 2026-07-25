package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
)

type fakeStore struct {
	present map[string]bool
	err     error
}

func (f *fakeStore) Exists(_ context.Context, ref string) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	return f.present[ref], nil
}

func (f *fakeStore) Store(context.Context, string, string) error { return nil }
func (f *fakeStore) Delete(context.Context, string) error        { return nil }
func (f *fakeStore) Stream(context.Context, string) (ports.AudioStream, int64, error) {
	return nil, 0, errors.New("not used")
}

type fakeRepo struct {
	track   *domain.Track
	updated *domain.Track
}

func (r *fakeRepo) GetByID(context.Context, domain.TrackId, shared.UserId) (*domain.Track, error) {
	return r.track, nil
}

func (r *fakeRepo) Update(_ context.Context, track *domain.Track) error {
	r.updated = track
	return nil
}

func testCandidate(t *testing.T) candidate {
	t.Helper()
	userID, err := shared.ParseUserId("11111111-1111-1111-1111-111111111111")
	if err != nil {
		t.Fatalf("parse user id: %v", err)
	}
	return candidate{
		trackID: domain.NewTrackId(),
		userID:  userID,
		title:   "Night Drive",
		artist:  "Ken Carson",
		album:   "UNRELEASED",
	}
}

func TestCandidateRefs_MatchesTheAcquisitionLayout(t *testing.T) {
	refs := candidateRefs(testCandidate(t))

	want := "11111111-1111-1111-1111-111111111111/Ken Carson/UNRELEASED/Night Drive.mp3"
	if refs[0] != want {
		t.Errorf("first ref = %q, want %q", refs[0], want)
	}
	if len(refs) != 2*len(extensions) {
		t.Errorf("got %d refs, want both layouts per extension (%d)", len(refs), 2*len(extensions))
	}
}

func TestCandidateRefs_AlsoTriesTheUnprefixedLibraryLayout(t *testing.T) {
	refs := candidateRefs(testCandidate(t))

	want := "Ken Carson/UNRELEASED/Night Drive.mp3"
	found := false
	for _, ref := range refs {
		if ref == want {
			found = true
		}
		if strings.HasPrefix(ref, "/") {
			t.Errorf("ref %q has a leading slash", ref)
		}
	}
	if !found {
		t.Errorf("no ref matched %q; got %v", want, refs)
	}
}

func TestReconcile_MarksReadyWhenTheObjectExists(t *testing.T) {
	c := testCandidate(t)
	ref := candidateRefs(c)[0]
	track, err := domain.NewTrack(c.userID, c.title, c.artist, c.album)
	if err != nil {
		t.Fatalf("new track: %v", err)
	}
	repo := &fakeRepo{track: track}

	got := reconcile(context.Background(), repo, &fakeStore{present: map[string]bool{ref: true}}, c, true)

	if got.err != nil {
		t.Fatalf("reconcile: %v", got.err)
	}
	if repo.updated == nil || !repo.updated.IsStreamable() {
		t.Fatal("track was not persisted as streamable")
	}
	if *repo.updated.AudioRef != ref {
		t.Errorf("audio ref = %q, want %q", *repo.updated.AudioRef, ref)
	}
}

func TestReconcile_FindsANonMp3Container(t *testing.T) {
	c := testCandidate(t)
	opus := candidateRefs(c)[2]
	repo := &fakeRepo{}

	got := reconcile(context.Background(), repo, &fakeStore{present: map[string]bool{opus: true}}, c, false)

	if got.ref != opus {
		t.Errorf("ref = %q, want %q", got.ref, opus)
	}
}

func TestReconcile_LeavesTheTrackAloneWhenNothingIsInStorage(t *testing.T) {
	c := testCandidate(t)
	repo := &fakeRepo{track: &domain.Track{}}

	got := reconcile(context.Background(), repo, &fakeStore{present: map[string]bool{}}, c, true)

	if got.ref != "" {
		t.Errorf("ref = %q, want empty", got.ref)
	}
	if repo.updated != nil {
		t.Error("track was persisted despite no object in storage")
	}
	if len(got.tried) != 2*len(extensions) {
		t.Errorf("reported %d attempted keys, want %d", len(got.tried), 2*len(extensions))
	}
}

func TestReconcile_DryRunNeverWrites(t *testing.T) {
	c := testCandidate(t)
	ref := candidateRefs(c)[0]
	repo := &fakeRepo{track: &domain.Track{}}

	got := reconcile(context.Background(), repo, &fakeStore{present: map[string]bool{ref: true}}, c, false)

	if got.ref != ref {
		t.Errorf("ref = %q, want %q", got.ref, ref)
	}
	if repo.updated != nil {
		t.Error("dry run persisted a track")
	}
}

func TestReconcile_SurfacesAStorageError(t *testing.T) {
	c := testCandidate(t)
	repo := &fakeRepo{track: &domain.Track{}}

	got := reconcile(context.Background(), repo, &fakeStore{err: errors.New("bucket unreachable")}, c, true)

	if got.err == nil {
		t.Fatal("expected the storage error to surface")
	}
	if repo.updated != nil {
		t.Error("track was persisted despite an unreadable bucket")
	}
}

func TestVerifyRefs_DryRunReportsWithoutWriting(t *testing.T) {
	c := testCandidate(t)
	c.storedRef = "Ken Carson/UNRELEASED/Night Drive.mp3"
	repo := &fakeRepo{track: &domain.Track{}}

	err := verifyRefs(context.Background(), repo, &fakeStore{present: map[string]bool{}}, []candidate{c}, false)

	if err != nil {
		t.Fatalf("verifyRefs: %v", err)
	}
	if repo.updated != nil {
		t.Error("dry run persisted a track")
	}
}

func TestVerifyRefs_MarksADanglingRefFailedOnApply(t *testing.T) {
	c := testCandidate(t)
	c.storedRef = "Ken Carson/UNRELEASED/Night Drive.mp3"
	track, err := domain.NewTrack(c.userID, c.title, c.artist, c.album)
	if err != nil {
		t.Fatalf("new track: %v", err)
	}
	if err := track.MarkReady(c.storedRef); err != nil {
		t.Fatalf("mark ready: %v", err)
	}
	repo := &fakeRepo{track: track}

	if err := verifyRefs(context.Background(), repo, &fakeStore{present: map[string]bool{}}, []candidate{c}, true); err != nil {
		t.Fatalf("verifyRefs: %v", err)
	}

	if repo.updated == nil {
		t.Fatal("track was not persisted")
	}
	if repo.updated.AcquisitionStatus != domain.AcquisitionFailed {
		t.Errorf("status = %v, want failed", repo.updated.AcquisitionStatus)
	}
	if repo.updated.AudioRef != nil {
		t.Error("audio ref should be cleared so nothing streams the dead key")
	}
}

func TestVerifyRefs_LeavesAResolvableRefAlone(t *testing.T) {
	c := testCandidate(t)
	c.storedRef = "Ken Carson/UNRELEASED/Night Drive.mp3"
	repo := &fakeRepo{track: &domain.Track{}}
	store := &fakeStore{present: map[string]bool{c.storedRef: true}}

	if err := verifyRefs(context.Background(), repo, store, []candidate{c}, true); err != nil {
		t.Fatalf("verifyRefs: %v", err)
	}

	if repo.updated != nil {
		t.Error("a healthy track was modified")
	}
}
