package service

import (
	"altune/go-api/internal/acquisition/adapters/catalogbridge"
	"altune/go-api/internal/acquisition/adapters/discoverybridge"
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"context"
	"encoding/json"
	"math"
	"testing"

	"github.com/google/uuid"
)

func TestUpdateTrackStep_Execute(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	track, err := domain.NewTrack(userId, "Song", "Artist", "Album")
	if err != nil {
		t.Fatalf("failed to create track: %v", err)
	}

	repo := newFakeTrackRepository()
	repo.tracks[track.ID.String()+":"+userId.String()] = track

	step := NewUpdateTrackStep(catalogbridge.NewTrackLedger(repo), userId, track.ID)
	ac := &AcquisitionContext{
		AudioRef: "user/artist/album/song.mp3",
	}

	_, execErr := step.Execute(context.Background(), ac, afterStore{})

	if execErr != nil {
		t.Fatalf("expected no error, got %v", execErr)
	}

	updated := storedTrack(t, repo, track)
	if updated.AcquisitionStatus != domain.AcquisitionReady {
		t.Errorf("track status = %v, want %v", updated.AcquisitionStatus, domain.AcquisitionReady)
	}
	if updated.AudioRef == nil || *updated.AudioRef != "user/artist/album/song.mp3" {
		t.Errorf("track AudioRef = %v, want %q", updated.AudioRef, "user/artist/album/song.mp3")
	}
}

func TestUpdateTrackStep_Execute_InfiniteProbeLeavesDurationUnknown(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	track, err := domain.NewTrack(userId, "Song", "Artist", "Album")
	if err != nil {
		t.Fatalf("failed to create track: %v", err)
	}
	repo := newFakeTrackRepository()
	repo.tracks[track.ID.String()+":"+userId.String()] = track
	step := NewUpdateTrackStep(catalogbridge.NewTrackLedger(repo), userId, track.ID)
	ac := &AcquisitionContext{AudioRef: "user/artist/album/song.mp3", ProbedDuration: math.Inf(1)}

	if _, execErr := step.Execute(context.Background(), ac, afterStore{}); execErr != nil {
		t.Fatalf("expected no error, got %v", execErr)
	}

	updated, ok := repo.tracks[track.ID.String()+":"+userId.String()]
	if !ok || updated == nil {
		t.Fatal("track missing after update")
	}
	if updated.AcquisitionStatus != domain.AcquisitionReady {
		t.Errorf("track status = %v, want %v", updated.AcquisitionStatus, domain.AcquisitionReady)
	}
	if updated.DurationSeconds != nil {
		t.Errorf("DurationSeconds = %v, want unset", *updated.DurationSeconds)
	}
}

func TestUpdateTrackStep_Execute_TrackNotFound(t *testing.T) {
	repo := newFakeTrackRepository()
	userId := shared.NewUserId(uuid.New())
	trackId := domain.NewTrackId()

	step := NewUpdateTrackStep(catalogbridge.NewTrackLedger(repo), userId, trackId)
	ac := &AcquisitionContext{
		AudioRef: "some/audio/ref.mp3",
	}

	_, err := step.Execute(context.Background(), ac, afterStore{})

	if err == nil {
		t.Fatal("expected error when track not found, got nil")
	}
	if got := err.Error(); got != "track not found for update" {
		t.Errorf("error = %q, want %q", got, "track not found for update")
	}
}

func TestUpdateTrackStep_Execute_EmptyAudioRef(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	track, err := domain.NewTrack(userId, "Song", "Artist", "Album")
	if err != nil {
		t.Fatalf("NewTrack: %v", err)
	}

	repo := newFakeTrackRepository()
	repo.tracks[track.ID.String()+":"+userId.String()] = track

	step := NewUpdateTrackStep(catalogbridge.NewTrackLedger(repo), userId, track.ID)
	ac := &AcquisitionContext{
		AudioRef: "",
	}

	_, err = step.Execute(context.Background(), ac, afterStore{})

	if err == nil {
		t.Fatal("expected error when audioRef is empty, got nil")
	}
}

func TestUpdateTrackStep_Rollback_RevertsToPending(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	track, err := domain.NewTrack(userId, "Song", "Artist", "Album")
	if err != nil {
		t.Fatalf("NewTrack: %v", err)
	}
	audioRef := "user/artist/album/song.mp3"
	_ = track.MarkReady(audioRef)

	repo := newFakeTrackRepository()
	repo.tracks[track.ID.String()+":"+userId.String()] = track

	step := NewUpdateTrackStep(catalogbridge.NewTrackLedger(repo), userId, track.ID)
	ac := &AcquisitionContext{}

	if err := step.Rollback(context.Background(), ac); err != nil {
		t.Fatalf("expected no error on rollback, got %v", err)
	}
	reverted := storedTrack(t, repo, track)
	if reverted.AcquisitionStatus != domain.AcquisitionPending {
		t.Errorf("track status after rollback = %v, want %v", reverted.AcquisitionStatus, domain.AcquisitionPending)
	}
}

func TestUpdateTrackStep_Name(t *testing.T) {
	step := NewUpdateTrackStep(nil, shared.UserId{}, domain.TrackId{})
	if got := step.Name(); got != "update_track" {
		t.Errorf("Name() = %q, want %q", got, "update_track")
	}
}

func TestUpdateTrackStep_DuplicateCompletionUnderSameRefSucceeds(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	repo := newFakeTrackRepository()
	track := seedTrackInRepo(t, repo, userId, markReadyWith("u/a/b/song.opus"))
	step := NewUpdateTrackStep(catalogbridge.NewTrackLedger(repo), userId, track.ID)

	if _, err := step.Execute(context.Background(), &AcquisitionContext{AudioRef: "u/a/b/song.opus"}, afterStore{}); err != nil {
		t.Fatalf("duplicate completion under the same ref: %v", err)
	}
	got := storedTrack(t, repo, track)
	if got.AcquisitionStatus != domain.AcquisitionReady || got.AudioRef == nil || *got.AudioRef != "u/a/b/song.opus" {
		t.Errorf("track = %v %v, want ready with the shared ref", got.AcquisitionStatus, got.AudioRef)
	}
}

func TestUpdateTrackStep_DuplicateCompletionUnderOtherRefIsRefused(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	repo := newFakeTrackRepository()
	track := seedTrackInRepo(t, repo, userId, markReadyWith("u/a/b/first.opus"))
	step := NewUpdateTrackStep(catalogbridge.NewTrackLedger(repo), userId, track.ID)

	_, err := step.Execute(context.Background(), &AcquisitionContext{AudioRef: "u/a/b/second.opus"}, afterStore{})

	if err == nil {
		t.Fatal("expected a duplicate completion to be refused")
	}
	got := storedTrack(t, repo, track)
	if got.AudioRef == nil || *got.AudioRef != "u/a/b/first.opus" {
		t.Errorf("AudioRef = %v, want the first completion's audio kept", got.AudioRef)
	}
}

func TestUpdateTrackStep_ReplaceSwapsReadyAudio(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	repo := newFakeTrackRepository()
	track := seedTrackInRepo(t, repo, userId, markReadyWith("u/a/b/old.opus"))
	step := NewUpdateTrackStep(catalogbridge.NewTrackLedger(repo), userId, track.ID)
	ac := &AcquisitionContext{AudioRef: "u/a/b/new.opus", Replace: ReplaceState{PreservedRef: "u/a/b/old.opus"}}

	if _, err := step.Execute(context.Background(), ac, afterStore{}); err != nil {
		t.Fatalf("replace update: %v", err)
	}
	got := storedTrack(t, repo, track)
	if got.AudioRef == nil || *got.AudioRef != "u/a/b/new.opus" {
		t.Errorf("AudioRef = %v, want the replacement audio", got.AudioRef)
	}
}

func TestUpdateTrackStep_Execute_StoresVerdictScoreAndSourceTitleAsEvidence(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	repo := newFakeTrackRepository()
	track := seedTrackInRepo(t, repo, userId, nil)
	ac := &AcquisitionContext{
		AudioRef: "user/artist/album/song.mp3",
		Selected: &ports.AudioCandidate{Title: "Song (Official Audio)", URL: "https://example.com/v"},
		Verdict:  AudioVerdict{Kind: VerdictHard, Score: 0.93},
	}

	if _, err := NewUpdateTrackStep(catalogbridge.NewTrackLedger(repo), userId, track.ID).Execute(context.Background(), ac, afterStore{}); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	updated := storedTrack(t, repo, track)
	var got Evidence
	if err := json.Unmarshal(updated.AcquisitionEvidence(), &got); err != nil {
		t.Fatalf("evidence not stored as JSON: %v (%q)", err, updated.AcquisitionEvidence())
	}
	if got.Verdict != "hard" || got.AcoustIDScore != 0.93 || got.SourceTitle != "Song (Official Audio)" {
		t.Errorf("evidence = %+v", got)
	}
	if updated.AcquisitionConfidence() != 0 {
		t.Errorf("confidence = %v, want 0 until computed", updated.AcquisitionConfidence())
	}
}

func TestUpdateTrackStep_Execute_NoStoredCandidateLeavesEvidenceEmpty(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	repo := newFakeTrackRepository()
	track := seedTrackInRepo(t, repo, userId, nil)

	if _, err := NewUpdateTrackStep(catalogbridge.NewTrackLedger(repo), userId, track.ID).Execute(context.Background(), &AcquisitionContext{AudioRef: "a.mp3"}, afterStore{}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if updated := storedTrack(t, repo, track); updated.AcquisitionEvidence() != nil {
		t.Errorf("evidence = %s, want none", updated.AcquisitionEvidence())
	}
}

func TestUpdateTrackStep_RollbackOnPendingTrackIsANoOp(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	repo := newFakeTrackRepository()
	track := seedTrackInRepo(t, repo, userId, nil)
	marker := track.AcquisitionStartedAt
	step := NewUpdateTrackStep(catalogbridge.NewTrackLedger(repo), userId, track.ID)

	if err := step.Rollback(context.Background(), &AcquisitionContext{}); err != nil {
		t.Fatalf("rollback of a pending track: %v", err)
	}
	got := storedTrack(t, repo, track)
	if got.AcquisitionStartedAt != marker {
		t.Error("rollback refreshed the in-flight marker of a track that was never marked ready")
	}
}

func TestUpdateTrackStep_Execute_PersistsTheISRCAcquisitionResolved(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	repo := newFakeTrackRepository()
	track := seedTrackInRepo(t, repo, userId, nil)
	ac := &AcquisitionContext{AudioRef: "a.mp3", Track: TrackRef{ISRC: "GBAYE0601498"}}

	if _, err := NewUpdateTrackStep(catalogbridge.NewTrackLedger(repo), userId, track.ID).Execute(context.Background(), ac, afterStore{}); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if got := storedTrack(t, repo, track).ISRC; got == nil || *got != "GBAYE0601498" {
		t.Errorf("ISRC = %v, want GBAYE0601498", got)
	}
}

const (
	drinkingISRC   = "CAA509814003"
	drinkingAlbum  = "5d6efd30-23c6-4a9b-85af-706ea2df9022"
	drinkingRemix  = "d3dc2825-20c0-4a4f-a963-baa589049146"
	drinkingAudio  = "user/bran-van-3000/drinking-in-la.opus"
	drinkingSource = "https://www.youtube.com/watch?v=-_5yNLsZ5Pk"
)

func completeWith(t *testing.T, ac *AcquisitionContext, storedISRC string) (*domain.Track, Evidence) {
	t.Helper()
	userId := shared.NewUserId(uuid.New())
	repo := newFakeTrackRepository()
	track := seedTrackInRepo(t, repo, userId, nil)
	if storedISRC != "" {
		track.AdoptISRC(storedISRC)
	}
	ac.AudioRef = drinkingAudio
	ac.Selected = &ports.AudioCandidate{Title: "Drinking in L.A.", URL: drinkingSource}
	if _, err := NewUpdateTrackStep(catalogbridge.NewTrackLedger(repo), userId, track.ID).Execute(context.Background(), ac, afterStore{}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	updated := storedTrack(t, repo, track)
	var evidence Evidence
	if err := json.Unmarshal(updated.AcquisitionEvidence(), &evidence); err != nil {
		t.Fatalf("evidence not stored as JSON: %v", err)
	}
	return updated, evidence
}

func assertStoredISRC(t *testing.T, track *domain.Track, want string) {
	t.Helper()
	switch got := track.ISRC; {
	case want == "" && got != nil && *got != "":
		t.Errorf("stored ISRC = %q, want none", *got)
	case want != "" && (got == nil || *got != want):
		t.Errorf("stored ISRC = %v, want %q", got, want)
	}
}

func TestUpdateTrackStep_AnUnprovenISRCIsNotWrittenBack(t *testing.T) {
	ac := &AcquisitionContext{
		BestEffort: true,
		Verdict:    AudioVerdict{Kind: VerdictUnknown},
		Identity:   ports.RecordingIdentity{ISRC: drinkingISRC, MBID: drinkingRemix, MBIDs: []string{drinkingRemix}},
	}

	track, evidence := completeWith(t, ac, "")

	assertStoredISRC(t, track, "")
	if evidence.ISRCSource != ISRCUnproven {
		t.Errorf("isrc_source = %q, want %q", evidence.ISRCSource, ISRCUnproven)
	}
}

func TestUpdateTrackStep_AFingerprintProvenISRCIsWrittenBack(t *testing.T) {
	ac := &AcquisitionContext{
		Verdict: AudioVerdict{Kind: VerdictHard, Score: 0.99, Surviving: []ports.LinkedRecording{{MBID: drinkingAlbum, Duration: 236}}},
		Identity: ports.RecordingIdentity{
			ISRC: drinkingISRC, MBID: drinkingAlbum, MBIDs: []string{drinkingAlbum},
		},
	}

	track, evidence := completeWith(t, ac, "")

	assertStoredISRC(t, track, drinkingISRC)
	if evidence.ISRCSource != ISRCFromFingerprint {
		t.Errorf("isrc_source = %q, want %q", evidence.ISRCSource, ISRCFromFingerprint)
	}
}

func TestUpdateTrackStep_ADoubtedReferenceDoesNotProveItsISRC(t *testing.T) {
	ac := &AcquisitionContext{
		Verdict: AudioVerdict{Kind: VerdictHard, Surviving: []ports.LinkedRecording{{MBID: drinkingAlbum}}},
		Identity: ports.RecordingIdentity{
			ISRC: drinkingISRC, MBID: drinkingAlbum, MBIDs: []string{drinkingAlbum}, ReferenceDoubted: true,
		},
	}

	track, evidence := completeWith(t, ac, "")

	assertStoredISRC(t, track, "")
	if evidence.ISRCSource != ISRCUnproven {
		t.Errorf("isrc_source = %q, want %q", evidence.ISRCSource, ISRCUnproven)
	}
}

func TestUpdateTrackStep_AHardVerdictOutsideTheReferenceSetDoesNotProveTheISRC(t *testing.T) {
	ac := &AcquisitionContext{
		Verdict:  AudioVerdict{Kind: VerdictHard, Surviving: []ports.LinkedRecording{{MBID: "some-other-recording"}}},
		Identity: ports.RecordingIdentity{ISRC: drinkingISRC, MBID: drinkingAlbum, MBIDs: []string{drinkingAlbum}},
	}

	track, _ := completeWith(t, ac, "")

	assertStoredISRC(t, track, "")
}

func TestUpdateTrackStep_ThePinnedRecordingsISRCIsWrittenBack(t *testing.T) {
	ac := &AcquisitionContext{
		BestEffort: true,
		Track:      TrackRef{CatalogMBID: drinkingAlbum},
		Verdict:    AudioVerdict{Kind: VerdictUnknown},
		Identity:   ports.RecordingIdentity{ISRC: drinkingISRC, MBID: drinkingAlbum, MBIDs: []string{drinkingAlbum}},
	}

	track, evidence := completeWith(t, ac, "")

	assertStoredISRC(t, track, drinkingISRC)
	if evidence.ISRCSource != ISRCFromPinnedRecording {
		t.Errorf("isrc_source = %q, want %q", evidence.ISRCSource, ISRCFromPinnedRecording)
	}
}

func TestUpdateTrackStep_AnExistingISRCIsNeverReplacedByAProvenOne(t *testing.T) {
	ac := &AcquisitionContext{
		Track:    TrackRef{ISRC: "USWB19901214"},
		Verdict:  AudioVerdict{Kind: VerdictHard, Surviving: []ports.LinkedRecording{{MBID: drinkingAlbum}}},
		Identity: ports.RecordingIdentity{ISRC: "DEUM71724939", MBID: drinkingAlbum, MBIDs: []string{drinkingAlbum}},
	}

	track, evidence := completeWith(t, ac, "USWB19901214")

	assertStoredISRC(t, track, "USWB19901214")
	if evidence.ISRCSource != ISRCFromCatalog {
		t.Errorf("isrc_source = %q, want %q", evidence.ISRCSource, ISRCFromCatalog)
	}
}

func TestUpdateTrackStep_DrinkingInLAWritesNoGuessedISRC(t *testing.T) {
	recorded := discoverybridge.NewRecordedResolver(
		&discoverybridge.RecordedSearchHit{Title: "Drinking in L.A.", Artist: "Bran Van 3000", ISRC: drinkingISRC, MBID: drinkingRemix, Duration: 236},
		[]discoverybridge.RecordedISRCRecording{{MBID: drinkingAlbum, Duration: 236}}, nil)
	ac := &AcquisitionContext{
		BestEffort: true,
		Track:      TrackRef{Title: "Drinking in L.A.", Artist: "Bran Van 3000", Duration: 236},
		Verdict:    AudioVerdict{Kind: VerdictUnknown},
	}
	ResolveIdentity(context.Background(), recorded, nil, ac)
	if ac.Identity.ISRC != drinkingISRC {
		t.Fatalf("resolver identity ISRC = %q, want the search hit's guess", ac.Identity.ISRC)
	}

	track, evidence := completeWith(t, ac, "")

	assertStoredISRC(t, track, "")
	if evidence.ISRCSource != ISRCUnproven {
		t.Errorf("isrc_source = %q, want %q", evidence.ISRCSource, ISRCUnproven)
	}
}
