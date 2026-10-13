package catalogbridge

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/catalog/domain"
	catalogports "altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
)

type fakeStore struct {
	tracks    map[domain.TrackId]*domain.Track
	updates   []int
	gets      int
	conflicts int
}

func newFakeStore(tracks ...*domain.Track) *fakeStore {
	s := &fakeStore{tracks: map[domain.TrackId]*domain.Track{}}
	for _, t := range tracks {
		s.tracks[t.ID] = t
	}
	return s
}

func (s *fakeStore) GetByID(_ context.Context, id domain.TrackId, userId shared.UserId) (*domain.Track, error) {
	s.gets++
	t, ok := s.tracks[id]
	if !ok || t.UserId != userId {
		return nil, nil
	}
	cp := *t
	return &cp, nil
}

func (s *fakeStore) Update(_ context.Context, track *domain.Track, expectedVersion int) error {
	s.updates = append(s.updates, expectedVersion)
	if s.conflicts > 0 {
		s.conflicts--
		return catalogports.ErrTrackVersionConflict
	}
	cur := s.tracks[track.ID]
	if cur.Version != expectedVersion {
		return catalogports.ErrTrackVersionConflict
	}
	cp := *track
	cp.Version = expectedVersion + 1
	s.tracks[track.ID] = &cp
	return nil
}

func (s *fakeStore) AudioRefInUse(context.Context, string, domain.TrackId) (bool, error) {
	return false, nil
}

func newTrack(t *testing.T, version int) (*domain.Track, shared.UserId) {
	t.Helper()
	userId := shared.NewUserId(uuid.New())
	track, err := domain.NewTrack(userId, "Title", "Artist", "Album")
	if err != nil {
		t.Fatal(err)
	}
	track.Version = version
	return track, userId
}

func TestGetMapsFieldsAndReturnsNilWhenMissing(t *testing.T) {
	track, userId := newTrack(t, 3)
	isrc := "USRC17607839"
	track.ISRC = &isrc
	if err := track.MarkReady("ref-1"); err != nil {
		t.Fatal(err)
	}
	ledger := NewTrackLedger(newFakeStore(track))

	snap, err := ledger.Get(context.Background(), track.ID, userId)
	if err != nil {
		t.Fatal(err)
	}
	if snap.ID != track.ID || snap.UserId != userId || snap.Title != "Title" || snap.Artist != "Artist" || snap.Album != "Album" {
		t.Fatalf("identity fields not mapped: %+v", snap)
	}
	if snap.ISRC == nil || *snap.ISRC != isrc || snap.AudioRef == nil || *snap.AudioRef != "ref-1" {
		t.Fatalf("optional fields not mapped: %+v", snap)
	}
	if snap.Status != ports.TrackReady || !snap.Streamable || snap.Version != 3 {
		t.Fatalf("status/streamable/version not mapped: %+v", snap)
	}

	missing, err := ledger.Get(context.Background(), domain.NewTrackId(), userId)
	if err != nil || missing != nil {
		t.Fatalf("want nil, nil for missing track, got %v, %v", missing, err)
	}
}

func TestCompleteMarksPendingTrackReady(t *testing.T) {
	track, userId := newTrack(t, 4)
	store := newFakeStore(track)
	ledger := NewTrackLedger(store)

	err := ledger.Complete(context.Background(), track.ID, userId, ports.Completion{
		AudioRef:           "ref-1",
		Duration:           215.5,
		Provenance:         shared.ProvenanceVerified,
		RejectedSourceKeys: []string{"bad-key"},
		SourceURL:          "https://example.test/a",
		Confidence:         0.9,
		Evidence:           json.RawMessage(`{"k":1}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(store.updates) != 1 || store.updates[0] != 4 {
		t.Fatalf("want one update at version 4, got %v", store.updates)
	}
	got := store.tracks[track.ID]
	if got.AcquisitionStatus != domain.AcquisitionReady || got.AudioRef == nil || *got.AudioRef != "ref-1" {
		t.Fatalf("track not ready: %+v", got)
	}
	if got.DurationSeconds == nil || *got.DurationSeconds != 215.5 {
		t.Fatalf("duration not set: %v", got.DurationSeconds)
	}
	if got.AcquisitionProvenance == nil || *got.AcquisitionProvenance != string(shared.ProvenanceVerified) {
		t.Fatalf("provenance not set: %v", got.AcquisitionProvenance)
	}
	if len(got.RejectedSourceKeys) != 1 || got.RejectedSourceKeys[0] != "bad-key" {
		t.Fatalf("rejected keys not set: %v", got.RejectedSourceKeys)
	}
	if got.AudioSourceURL == nil || *got.AudioSourceURL != "https://example.test/a" {
		t.Fatalf("source url not set: %v", got.AudioSourceURL)
	}
	if got.ConfidenceScore == nil || *got.ConfidenceScore != 0.9 || string(got.EvidenceJSON) != `{"k":1}` {
		t.Fatalf("confidence not set: %v %s", got.ConfidenceScore, got.EvidenceJSON)
	}
}

func TestCompleteWithReplaceAudioReplacesReadyTrack(t *testing.T) {
	track, userId := newTrack(t, 1)
	if err := track.MarkReady("old-ref"); err != nil {
		t.Fatal(err)
	}
	store := newFakeStore(track)
	ledger := NewTrackLedger(store)

	if err := ledger.Complete(context.Background(), track.ID, userId, ports.Completion{AudioRef: "new-ref", ReplaceAudio: true}); err != nil {
		t.Fatal(err)
	}
	got := store.tracks[track.ID]
	if got.AudioRef == nil || *got.AudioRef != "new-ref" || got.AcquisitionStatus != domain.AcquisitionReady {
		t.Fatalf("audio not replaced: %+v", got)
	}
}

func TestCompleteOnFailedTrackIsIllegalTransition(t *testing.T) {
	track, userId := newTrack(t, 1)
	if err := track.FailAcquisition("boom"); err != nil {
		t.Fatal(err)
	}
	store := newFakeStore(track)
	ledger := NewTrackLedger(store)

	err := ledger.Complete(context.Background(), track.ID, userId, ports.Completion{AudioRef: "ref", ReplaceAudio: true})
	if !errors.Is(err, ports.ErrIllegalTransition) {
		t.Fatalf("want ErrIllegalTransition, got %v", err)
	}
	if !errors.Is(err, domain.ErrIllegalAcquisitionTransition) {
		t.Fatalf("want domain cause preserved, got %v", err)
	}
	if len(store.updates) != 0 {
		t.Fatalf("no update expected, got %v", store.updates)
	}
	if store.gets != 1 {
		t.Fatalf("want exactly 1 load, got %d", store.gets)
	}
}

func TestRevertAtUpdateConflictIsNotRetried(t *testing.T) {
	track, userId := newTrack(t, 5)
	if err := track.FailAcquisition("boom"); err != nil {
		t.Fatal(err)
	}
	store := newFakeStore(track)
	store.conflicts = 1
	ledger := NewTrackLedger(store)

	err := ledger.RevertAt(context.Background(), ports.TrackSnapshot{ID: track.ID, UserId: userId, Version: 5})
	if !errors.Is(err, catalogports.ErrTrackVersionConflict) {
		t.Fatalf("want conflict, got %v", err)
	}
	if len(store.updates) != 1 || store.gets != 1 {
		t.Fatalf("want one load and one update, got %d loads %v updates", store.gets, store.updates)
	}
}

func TestMissingTrackReturnsNotFound(t *testing.T) {
	_, userId := newTrack(t, 1)
	ledger := NewTrackLedger(newFakeStore())

	err := ledger.Fail(context.Background(), domain.NewTrackId(), userId, "boom")
	if !errors.Is(err, ports.ErrTrackNotFound) {
		t.Fatalf("want ErrTrackNotFound, got %v", err)
	}
}

func TestFailAndRevertTransition(t *testing.T) {
	track, userId := newTrack(t, 1)
	store := newFakeStore(track)
	ledger := NewTrackLedger(store)

	if err := ledger.Fail(context.Background(), track.ID, userId, "boom"); err != nil {
		t.Fatal(err)
	}
	if got := store.tracks[track.ID]; got.AcquisitionStatus != domain.AcquisitionFailed {
		t.Fatalf("want failed, got %v", got.AcquisitionStatus)
	}
	if err := ledger.Revert(context.Background(), track.ID, userId); err != nil {
		t.Fatal(err)
	}
	if got := store.tracks[track.ID]; got.AcquisitionStatus != domain.AcquisitionPending {
		t.Fatalf("want pending, got %v", got.AcquisitionStatus)
	}
}

func TestVersionConflictIsRetried(t *testing.T) {
	track, userId := newTrack(t, 2)
	store := newFakeStore(track)
	store.conflicts = 1
	ledger := NewTrackLedger(store)

	if err := ledger.Fail(context.Background(), track.ID, userId, "boom"); err != nil {
		t.Fatal(err)
	}
	if len(store.updates) != 2 {
		t.Fatalf("want 2 update attempts, got %v", store.updates)
	}
	if got := store.tracks[track.ID]; got.AcquisitionStatus != domain.AcquisitionFailed {
		t.Fatalf("want failed, got %v", got.AcquisitionStatus)
	}
}

func TestVersionConflictExhaustsAttempts(t *testing.T) {
	track, userId := newTrack(t, 2)
	store := newFakeStore(track)
	store.conflicts = 100
	ledger := NewTrackLedger(store)

	err := ledger.Fail(context.Background(), track.ID, userId, "boom")
	if !errors.Is(err, catalogports.ErrTrackVersionConflict) {
		t.Fatalf("want conflict, got %v", err)
	}
	if len(store.updates) != maxAttempts {
		t.Fatalf("want %d attempts, got %d", maxAttempts, len(store.updates))
	}
}

func TestRevertAtStaleVersionReturnsConflictWithoutRetry(t *testing.T) {
	track, userId := newTrack(t, 5)
	if err := track.FailAcquisition("boom"); err != nil {
		t.Fatal(err)
	}
	store := newFakeStore(track)
	ledger := NewTrackLedger(store)

	err := ledger.RevertAt(context.Background(), ports.TrackSnapshot{ID: track.ID, UserId: userId, Version: 4})
	if !errors.Is(err, catalogports.ErrTrackVersionConflict) {
		t.Fatalf("want conflict, got %v", err)
	}
	if len(store.updates) != 0 {
		t.Fatalf("no update expected, got %v", store.updates)
	}
}

func TestRevertAtRevertsAtSnapshotVersion(t *testing.T) {
	track, userId := newTrack(t, 5)
	if err := track.FailAcquisition("boom"); err != nil {
		t.Fatal(err)
	}
	store := newFakeStore(track)
	ledger := NewTrackLedger(store)

	if err := ledger.RevertAt(context.Background(), ports.TrackSnapshot{ID: track.ID, UserId: userId, Version: 5}); err != nil {
		t.Fatal(err)
	}
	if len(store.updates) != 1 || store.updates[0] != 5 {
		t.Fatalf("want one update at version 5, got %v", store.updates)
	}
	if got := store.tracks[track.ID]; got.AcquisitionStatus != domain.AcquisitionPending {
		t.Fatalf("want pending, got %v", got.AcquisitionStatus)
	}
}
