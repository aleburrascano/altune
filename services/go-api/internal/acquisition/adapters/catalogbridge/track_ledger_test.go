package catalogbridge

import (
	acqdomain "altune/go-api/internal/acquisition/domain"
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/catalog/domain"
	catalogports "altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
	"context"
	"encoding/json"
	"errors"
	"reflect"
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

func storedTrack(t *testing.T, s *fakeStore, id domain.TrackId) *domain.Track {
	t.Helper()
	got, ok := s.tracks[id]
	if !ok || got == nil {
		t.Fatalf("track %v not stored", id)
	}
	return got
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
	cur, ok := s.tracks[track.ID]
	if !ok || cur == nil {
		return catalogports.ErrTrackMissing
	}
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
	if snap == nil {
		t.Fatal("want snapshot, got nil")
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

	err := ledger.Complete(context.Background(), track.ID, userId, 0, ports.Completion{
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
	got := storedTrack(t, store, track.ID)
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

	if err := ledger.Complete(context.Background(), track.ID, userId, 0, ports.Completion{AudioRef: "new-ref", ReplaceAudio: true}); err != nil {
		t.Fatal(err)
	}
	got := storedTrack(t, store, track.ID)
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

	err := ledger.Complete(context.Background(), track.ID, userId, 0, ports.Completion{AudioRef: "ref", ReplaceAudio: true})
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

	err := ledger.Fail(context.Background(), domain.NewTrackId(), userId, 0, "boom")
	if !errors.Is(err, ports.ErrTrackNotFound) {
		t.Fatalf("want ErrTrackNotFound, got %v", err)
	}
}

func TestFailAndRevertTransition(t *testing.T) {
	track, userId := newTrack(t, 1)
	store := newFakeStore(track)
	ledger := NewTrackLedger(store)

	if err := ledger.Fail(context.Background(), track.ID, userId, 0, "boom"); err != nil {
		t.Fatal(err)
	}
	if got := storedTrack(t, store, track.ID); got.AcquisitionStatus != domain.AcquisitionFailed {
		t.Fatalf("want failed, got %v", got.AcquisitionStatus)
	}
	if err := ledger.Revert(context.Background(), track.ID, userId, 0); err != nil {
		t.Fatal(err)
	}
	if got := storedTrack(t, store, track.ID); got.AcquisitionStatus != domain.AcquisitionPending {
		t.Fatalf("want pending, got %v", got.AcquisitionStatus)
	}
}

func TestVersionConflictIsRetried(t *testing.T) {
	track, userId := newTrack(t, 2)
	store := newFakeStore(track)
	store.conflicts = 1
	ledger := NewTrackLedger(store)

	if err := ledger.Fail(context.Background(), track.ID, userId, 0, "boom"); err != nil {
		t.Fatal(err)
	}
	if len(store.updates) != 2 {
		t.Fatalf("want 2 update attempts, got %v", store.updates)
	}
	if got := storedTrack(t, store, track.ID); got.AcquisitionStatus != domain.AcquisitionFailed {
		t.Fatalf("want failed, got %v", got.AcquisitionStatus)
	}
}

func TestVersionConflictExhaustsAttempts(t *testing.T) {
	track, userId := newTrack(t, 2)
	store := newFakeStore(track)
	store.conflicts = 100
	ledger := NewTrackLedger(store)

	err := ledger.Fail(context.Background(), track.ID, userId, 0, "boom")
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
	if got := storedTrack(t, store, track.ID); got.AcquisitionStatus != domain.AcquisitionPending {
		t.Fatalf("want pending, got %v", got.AcquisitionStatus)
	}
}

func completeWithAudioLength(t *testing.T, catalogSeconds, audioSeconds float64) *domain.Track {
	t.Helper()
	track, userId := newTrack(t, 1)
	if err := track.SetCatalogDuration(catalogSeconds); err != nil {
		t.Fatal(err)
	}
	store := newFakeStore(track)
	err := NewTrackLedger(store).Complete(context.Background(), track.ID, userId, 0, ports.Completion{AudioRef: "ref", Duration: audioSeconds})
	if err != nil {
		t.Fatal(err)
	}
	return storedTrack(t, store, track.ID)
}

func TestCompleteKeepsTheCatalogLengthBesideTheFileLength(t *testing.T) {
	got := completeWithAudioLength(t, 235, 137)

	if got.CatalogDurationSeconds == nil || *got.CatalogDurationSeconds != 235 {
		t.Errorf("catalog length = %v, want 235", got.CatalogDurationSeconds)
	}
	if got.DurationSeconds == nil || *got.DurationSeconds != 137 {
		t.Errorf("file length = %v, want 137", got.DurationSeconds)
	}
}

func TestGetNeverAimsAtTheLastMeasuredFile(t *testing.T) {
	track, userId := newTrack(t, 1)
	if err := track.SetDuration(112); err != nil {
		t.Fatal(err)
	}

	snap, err := NewTrackLedger(newFakeStore(track)).Get(context.Background(), track.ID, userId)
	if err != nil {
		t.Fatal(err)
	}
	if snap == nil {
		t.Fatal("want snapshot, got nil")
	}

	if snap.CatalogDurationSeconds != nil {
		t.Errorf("target length = %v, want none: the measured file length is not a target", *snap.CatalogDurationSeconds)
	}
	if snap.StoredDurationSeconds == nil || *snap.StoredDurationSeconds != 112 {
		t.Errorf("StoredDurationSeconds = %v, want the measured 112", snap.StoredDurationSeconds)
	}
}

func TestGetAimsADownloadAtTheCatalogLengthNotTheStoredFile(t *testing.T) {
	tests := []struct {
		name    string
		catalog float64
		want    float64
	}{
		{"catalog length known", 235, 235},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			track, userId := newTrack(t, 1)
			if err := track.SetCatalogDuration(tt.catalog); err != nil {
				t.Fatal(err)
			}
			if err := track.SetDuration(137); err != nil {
				t.Fatal(err)
			}
			snap, err := NewTrackLedger(newFakeStore(track)).Get(context.Background(), track.ID, userId)
			if err != nil {
				t.Fatal(err)
			}
			if snap.CatalogDurationSeconds == nil || *snap.CatalogDurationSeconds != tt.want {
				t.Fatalf("target length = %v, want %v", snap.CatalogDurationSeconds, tt.want)
			}
		})
	}
}

func TestCompleteFlagsAudioOffTheCatalogLengthBeyondTheTolerance(t *testing.T) {
	tests := []struct {
		name     string
		catalog  float64
		audio    float64
		wantFlag bool
	}{
		{name: "short by more than the default", catalog: 235, audio: 137, wantFlag: true},
		{name: "long by more than the default", catalog: 125, audio: 172, wantFlag: true},
		{name: "exactly at the allowance", catalog: 200, audio: 205, wantFlag: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := completeWithAudioLength(t, tt.catalog, tt.audio)

			if got.AudioLengthMismatch != tt.wantFlag {
				t.Errorf("AudioLengthMismatch = %v, want %v", got.AudioLengthMismatch, tt.wantFlag)
			}
		})
	}
}

func TestComplete_FlagsAMismatchOutsideTheAuthoritativeWindow(t *testing.T) {
	tests := []struct {
		name     string
		catalog  float64
		audio    float64
		wantFlag bool
	}{
		{name: "400 s track 11 s off is inside 3%", catalog: 400, audio: 411, wantFlag: false},
		{name: "400 s track 13 s off is outside 3%", catalog: 400, audio: 413, wantFlag: true},
		{name: "200 s track 8 s off is outside the 5 s slack", catalog: 200, audio: 208, wantFlag: true},
		{name: "200 s track 5 s short is inside", catalog: 200, audio: 195, wantFlag: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := completeWithAudioLength(t, tt.catalog, tt.audio)

			if got.AudioLengthMismatch != tt.wantFlag {
				t.Errorf("AudioLengthMismatch = %v, want %v", got.AudioLengthMismatch, tt.wantFlag)
			}
		})
	}
}

func TestCompleteDoesNotFlagATrackWithoutACatalogLength(t *testing.T) {
	track, userId := newTrack(t, 1)
	store := newFakeStore(track)

	err := NewTrackLedger(store).Complete(context.Background(), track.ID, userId, 0, ports.Completion{AudioRef: "ref", Duration: 137})
	if err != nil {
		t.Fatal(err)
	}

	if storedTrack(t, store, track.ID).AudioLengthMismatch {
		t.Error("a track with no catalog length must not be flagged")
	}
}

func TestCompleteClearsTheFlagWhenReplacementAudioMatches(t *testing.T) {
	track, userId := newTrack(t, 1)
	if err := track.SetCatalogDuration(235); err != nil {
		t.Fatal(err)
	}
	if err := track.MarkReady("old"); err != nil {
		t.Fatal(err)
	}
	track.AudioLengthMismatch = true
	store := newFakeStore(track)

	err := NewTrackLedger(store).Complete(context.Background(), track.ID, userId, 0, ports.Completion{AudioRef: "new", ReplaceAudio: true, Duration: 236})
	if err != nil {
		t.Fatal(err)
	}

	if storedTrack(t, store, track.ID).AudioLengthMismatch {
		t.Error("flag must clear once the replacement audio is in tolerance")
	}
}

func TestCompletePersistsTheResolvedISRCOnlyWhenTheTrackHasNone(t *testing.T) {
	existing := "USUM71100001"
	tests := []struct {
		name string
		have *string
		got  string
		want string
	}{
		{name: "adopts the resolved isrc", have: nil, got: "GBAYE0601498", want: "GBAYE0601498"},
		{name: "keeps the isrc the track has", have: &existing, got: "GBAYE0601498", want: existing},
		{name: "ignores an empty resolution", have: nil, got: "", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			track, userId := newTrack(t, 1)
			track.ISRC = tt.have
			store := newFakeStore(track)

			err := NewTrackLedger(store).Complete(context.Background(), track.ID, userId, 0, ports.Completion{AudioRef: "ref", ISRC: tt.got})
			if err != nil {
				t.Fatal(err)
			}

			stored := storedTrack(t, store, track.ID).ISRC
			if tt.want == "" {
				if stored != nil {
					t.Errorf("ISRC = %q, want none", *stored)
				}
				return
			}
			if stored == nil || *stored != tt.want {
				t.Errorf("ISRC = %v, want %q", stored, tt.want)
			}
		})
	}
}

func TestGetExposesTheCatalogIdentity(t *testing.T) {
	track, userId := newTrack(t, 1)
	mbid := "5b11f4ce-a62d-471e-81fc-a69a8278c7da"
	explicit := true
	sources := []domain.CatalogSource{{Provider: "deezer", ExternalID: "42"}}
	if err := track.SetCatalogIdentity(&mbid, &explicit, sources); err != nil {
		t.Fatal(err)
	}
	if err := track.SetCatalogDuration(226); err != nil {
		t.Fatal(err)
	}

	snap, err := NewTrackLedger(newFakeStore(track)).Get(context.Background(), track.ID, userId)
	if err != nil || snap == nil {
		t.Fatalf("Get = %v, %v", snap, err)
	}
	if snap.CatalogMBID == nil || *snap.CatalogMBID != mbid {
		t.Errorf("CatalogMBID = %v, want %q", snap.CatalogMBID, mbid)
	}
	if snap.CatalogExplicit == nil || !*snap.CatalogExplicit {
		t.Errorf("CatalogExplicit = %v, want true", snap.CatalogExplicit)
	}
	if len(snap.CatalogSources) != 1 || snap.CatalogSources[0] != (ports.CatalogSource{Provider: "deezer", ExternalID: "42"}) {
		t.Errorf("CatalogSources = %+v", snap.CatalogSources)
	}
	if snap.CatalogDurationSeconds == nil || *snap.CatalogDurationSeconds != 226 {
		t.Errorf("CatalogDurationSeconds = %v, want 226", snap.CatalogDurationSeconds)
	}
}

func TestGetKeepsTheMeasuredFileLengthBesideTheCatalogLength(t *testing.T) {
	track, userId := newTrack(t, 1)
	measured, catalog := 112.0, 128.0
	track.DurationSeconds = &measured
	track.CatalogDurationSeconds = &catalog
	ledger := NewTrackLedger(newFakeStore(track))

	snap, err := ledger.Get(context.Background(), track.ID, userId)
	if err != nil {
		t.Fatal(err)
	}
	if snap == nil {
		t.Fatal("want snapshot, got nil")
	}
	if snap.StoredDurationSeconds == nil || *snap.StoredDurationSeconds != 112 {
		t.Fatalf("StoredDurationSeconds = %v, want the measured 112 not the catalog 128", snap.StoredDurationSeconds)
	}
	if snap.CatalogDurationSeconds == nil || *snap.CatalogDurationSeconds != 128 {
		t.Fatalf("CatalogDurationSeconds = %v, want the catalog target 128", snap.CatalogDurationSeconds)
	}
}

func TestGetExposesThePickedSourceURL(t *testing.T) {
	track, userId := newTrack(t, 1)
	const pick = "https://soundcloud.com/artist/track"
	track.CatalogSources = []domain.CatalogSource{{Provider: "soundcloud", ExternalID: pick, URL: pick}}

	snap, err := NewTrackLedger(newFakeStore(track)).Get(context.Background(), track.ID, userId)
	if err != nil || snap == nil {
		t.Fatalf("Get: %v, %v", snap, err)
	}
	want := []ports.CatalogSource{{Provider: "soundcloud", ExternalID: pick, URL: pick}}
	if !reflect.DeepEqual(snap.CatalogSources, want) {
		t.Errorf("CatalogSources = %+v, want %+v", snap.CatalogSources, want)
	}
}

func TestTrackLedger_CompleteWithAStaleFenceIsLeaseLost(t *testing.T) {
	track, userId := newTrack(t, 4)
	track.AcquisitionFence = 3
	store := newFakeStore(track)
	ledger := NewTrackLedger(store)

	err := ledger.Complete(context.Background(), track.ID, userId, 2, ports.Completion{AudioRef: "ref-1"})

	if !errors.Is(err, acqdomain.ErrLeaseLost) {
		t.Fatalf("err = %v, want ErrLeaseLost", err)
	}
	if got := storedTrack(t, store, track.ID); got.AcquisitionStatus != domain.AcquisitionPending || len(store.updates) != 0 {
		t.Errorf("status = %v, updates = %v, want the track untouched", got.AcquisitionStatus, store.updates)
	}
}

func TestTrackLedger_FailAndRevertWithAStaleFenceAreLeaseLost(t *testing.T) {
	track, userId := newTrack(t, 1)
	track.AcquisitionFence = 3
	store := newFakeStore(track)
	ledger := NewTrackLedger(store)

	failErr := ledger.Fail(context.Background(), track.ID, userId, 2, "boom")
	revertErr := ledger.Revert(context.Background(), track.ID, userId, 2)

	if !errors.Is(failErr, acqdomain.ErrLeaseLost) || !errors.Is(revertErr, acqdomain.ErrLeaseLost) {
		t.Fatalf("Fail = %v, Revert = %v, want ErrLeaseLost from both", failErr, revertErr)
	}
	if got := storedTrack(t, store, track.ID); got.AcquisitionStatus != domain.AcquisitionPending || len(store.updates) != 0 {
		t.Errorf("status = %v, updates = %v, want the track untouched", got.AcquisitionStatus, store.updates)
	}
}

func TestTrackLedger_WritesThatOwnTheLeaseOrCarryNoFenceGoThrough(t *testing.T) {
	cases := []struct {
		name         string
		heldFence    ports.Fence
		trackedFence int
	}{
		{"the held fence is the track's", 3, 3},
		{"no fence is held", 0, 3},
		{"the track tracks no lease", 3, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			track, userId := newTrack(t, 1)
			track.AcquisitionFence = tc.trackedFence
			store := newFakeStore(track)

			err := NewTrackLedger(store).Fail(context.Background(), track.ID, userId, tc.heldFence, "boom")
			if err != nil {
				t.Fatal(err)
			}
			if got := storedTrack(t, store, track.ID); got.AcquisitionStatus != domain.AcquisitionFailed {
				t.Errorf("status = %v, want failed", got.AcquisitionStatus)
			}
		})
	}
}

func TestTrackLedger_AClaimBetweenReadAndUpdateFencesOutTheWriter(t *testing.T) {
	track, userId := newTrack(t, 1)
	track.AcquisitionFence = 2
	store := &reclaimingStore{fakeStore: newFakeStore(track)}

	err := NewTrackLedger(store).Complete(context.Background(), track.ID, userId, 2, ports.Completion{AudioRef: "ref-1"})

	if !errors.Is(err, acqdomain.ErrLeaseLost) {
		t.Fatalf("err = %v, want ErrLeaseLost once the retry re-reads the new owner's fence", err)
	}
	if got := storedTrack(t, store.fakeStore, track.ID); got.AcquisitionStatus != domain.AcquisitionPending {
		t.Errorf("status = %v, want pending", got.AcquisitionStatus)
	}
}

type reclaimingStore struct {
	*fakeStore
	reclaimed bool
}

func (s *reclaimingStore) Update(ctx context.Context, track *domain.Track, expectedVersion int) error {
	if !s.reclaimed {
		s.reclaimed = true
		stored, ok := s.tracks[track.ID]
		if !ok || stored == nil {
			return catalogports.ErrTrackMissing
		}
		current := *stored
		current.AcquisitionFence++
		current.Version++
		s.tracks[track.ID] = &current
	}
	return s.fakeStore.Update(ctx, track, expectedVersion)
}

func TestTrackLedger_AFenceAheadOfTheTracksIsAlsoLeaseLost(t *testing.T) {
	track, userId := newTrack(t, 1)
	track.AcquisitionFence = 3
	store := newFakeStore(track)

	err := NewTrackLedger(store).Complete(context.Background(), track.ID, userId, 4, ports.Completion{AudioRef: "ref-1"})

	if !errors.Is(err, acqdomain.ErrLeaseLost) {
		t.Fatalf("err = %v, want ErrLeaseLost: a fence that differs either way is not the owner", err)
	}
	if got := storedTrack(t, store, track.ID); got.AcquisitionStatus != domain.AcquisitionPending || got.AudioRef != nil {
		t.Errorf("status = %v, audio_ref = %v, want the track pending with no audio", got.AcquisitionStatus, got.AudioRef)
	}
}

func TestTrackLedger_AStaleReplaceKeepsTheTrackOnItsServingAudio(t *testing.T) {
	track, userId := newTrack(t, 1)
	if err := track.MarkReady("old-ref"); err != nil {
		t.Fatal(err)
	}
	track.AcquisitionFence = 3
	store := newFakeStore(track)

	err := NewTrackLedger(store).Complete(context.Background(), track.ID, userId, 2, ports.Completion{AudioRef: "new-ref", ReplaceAudio: true})

	if !errors.Is(err, acqdomain.ErrLeaseLost) {
		t.Fatalf("err = %v, want ErrLeaseLost", err)
	}
	got := storedTrack(t, store, track.ID)
	if got.AcquisitionStatus != domain.AcquisitionReady || got.AudioRef == nil || *got.AudioRef != "old-ref" {
		t.Errorf("status = %v, audio_ref = %v, want ready on old-ref", got.AcquisitionStatus, got.AudioRef)
	}
	if len(store.updates) != 0 {
		t.Errorf("updates = %v, want none", store.updates)
	}
}

func TestTrackLedger_AStaleRevertLeavesAFailedTrackFailedWithItsReason(t *testing.T) {
	track, userId := newTrack(t, 1)
	if err := track.FailAcquisition("boom"); err != nil {
		t.Fatal(err)
	}
	track.AcquisitionFence = 3
	store := newFakeStore(track)

	err := NewTrackLedger(store).Revert(context.Background(), track.ID, userId, 2)

	if !errors.Is(err, acqdomain.ErrLeaseLost) {
		t.Fatalf("err = %v, want ErrLeaseLost", err)
	}
	got := storedTrack(t, store, track.ID)
	if got.AcquisitionStatus != domain.AcquisitionFailed || got.FailureReason == nil || *got.FailureReason != "boom" {
		t.Errorf("status = %v, reason = %v, want failed with boom", got.AcquisitionStatus, got.FailureReason)
	}
	if len(store.updates) != 0 {
		t.Errorf("updates = %v, want none", store.updates)
	}
}

func TestTrackLedger_CompleteAndRevertWithoutAFenceOnEitherSideGoThrough(t *testing.T) {
	cases := []struct {
		name         string
		heldFence    ports.Fence
		trackedFence int
	}{
		{"no fence is held", 0, 3},
		{"the track tracks no lease", 3, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name+": complete", func(t *testing.T) {
			track, userId := newTrack(t, 1)
			track.AcquisitionFence = tc.trackedFence
			store := newFakeStore(track)

			if err := NewTrackLedger(store).Complete(context.Background(), track.ID, userId, tc.heldFence, ports.Completion{AudioRef: "ref-1"}); err != nil {
				t.Fatal(err)
			}
			got := storedTrack(t, store, track.ID)
			if got.AcquisitionStatus != domain.AcquisitionReady || got.AudioRef == nil || *got.AudioRef != "ref-1" {
				t.Errorf("status = %v, audio_ref = %v, want ready on ref-1", got.AcquisitionStatus, got.AudioRef)
			}
		})
		t.Run(tc.name+": revert", func(t *testing.T) {
			track, userId := newTrack(t, 1)
			if err := track.FailAcquisition("boom"); err != nil {
				t.Fatal(err)
			}
			track.AcquisitionFence = tc.trackedFence
			store := newFakeStore(track)

			if err := NewTrackLedger(store).Revert(context.Background(), track.ID, userId, tc.heldFence); err != nil {
				t.Fatal(err)
			}
			if got := storedTrack(t, store, track.ID); got.AcquisitionStatus != domain.AcquisitionPending {
				t.Errorf("status = %v, want pending", got.AcquisitionStatus)
			}
		})
	}
}

func TestTrackLedger_AClaimBetweenReadAndUpdateStopsTheOldOwnerFailingTheTrack(t *testing.T) {
	track, userId := newTrack(t, 1)
	track.AcquisitionFence = 2
	store := &reclaimingStore{fakeStore: newFakeStore(track)}

	err := NewTrackLedger(store).Fail(context.Background(), track.ID, userId, 2, "boom")

	if !errors.Is(err, acqdomain.ErrLeaseLost) {
		t.Fatalf("err = %v, want ErrLeaseLost", err)
	}
	got := storedTrack(t, store.fakeStore, track.ID)
	if got.AcquisitionStatus != domain.AcquisitionPending || got.FailureReason != nil {
		t.Errorf("status = %v, reason = %v, want pending with no failure", got.AcquisitionStatus, got.FailureReason)
	}
	if got.AcquisitionFence != 3 {
		t.Errorf("fence = %d, want the new owner's 3", got.AcquisitionFence)
	}
}

func TestTrackLedger_RevertAtIgnoresTheLease(t *testing.T) {
	track, userId := newTrack(t, 5)
	if err := track.FailAcquisition("boom"); err != nil {
		t.Fatal(err)
	}
	track.AcquisitionFence = 3
	store := newFakeStore(track)

	if err := NewTrackLedger(store).RevertAt(context.Background(), ports.TrackSnapshot{ID: track.ID, UserId: userId, Version: 5}); err != nil {
		t.Fatal(err)
	}
	if got := storedTrack(t, store, track.ID); got.AcquisitionStatus != domain.AcquisitionPending {
		t.Errorf("status = %v, want pending", got.AcquisitionStatus)
	}
}
