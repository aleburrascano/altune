package service

import (
	"altune/go-api/internal/acquisition/internal/adapters/catalogbridge"
	"altune/go-api/internal/acquisition/internal/core/grade"
	"altune/go-api/internal/acquisition/internal/core/ports"
	"altune/go-api/internal/acquisition/internal/core/ranking"
	"altune/go-api/internal/acquisition/service/upgrade"
	"altune/go-api/internal/catalog/domain"
	shared "altune/go-api/internal/shared/kernel"
	"context"
	"encoding/json"
	"errors"
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
	ac.adopt(context.Background(), Evidence{Verdict: "hard", AcoustIDScore: 0.93, SourceTitle: "Song (Official Audio)"}, 0)

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

func TestUpdateTrackStep_Execute_StoresSourceKindGenerationAndGradePerKind(t *testing.T) {
	cases := []struct {
		name      string
		kind      ports.CandidateKind
		channel   string
		verdict   VerdictKind
		wantKind  grade.SourceKind
		wantGrade grade.Grade
	}{
		{"art track proven by fingerprint", ports.KindArtTrack, ranking.ChannelTopic, VerdictHard, grade.ArtTrack, grade.A},
		{"official video proven by fingerprint", ports.KindPinnedVideo, ranking.ChannelTopic, VerdictHard, grade.OfficialVideo, grade.A},
		{"artist's own upload proven by fingerprint", ports.KindUpload, ranking.ChannelArtist, VerdictHard, grade.LabelUpload, grade.A},
		{"fan upload proven by fingerprint", ports.KindUpload, ranking.ChannelOther, VerdictHard, grade.FanUpload, grade.B},
		{"art track the fingerprint says is another version", ports.KindArtTrack, ranking.ChannelTopic, VerdictOtherVersion, grade.ArtTrack, grade.F},
		{"fan upload with no fingerprint answer", ports.KindUpload, ranking.ChannelOther, VerdictUnknown, grade.FanUpload, grade.D},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			userId := shared.NewUserId(uuid.New())
			repo := newFakeTrackRepository()
			track := seedTrackInRepo(t, repo, userId, nil)
			ac := &AcquisitionContext{
				AudioRef: drinkingAudio,
				Selected: &ports.AudioCandidate{Title: "Song", URL: drinkingSource, Kind: tc.kind, ObservedOMV: tc.kind == ports.KindPinnedVideo},
				Verdict:  AudioVerdict{Kind: tc.verdict},
				Evidence: Evidence{Channel: tc.channel},
			}

			if _, err := NewUpdateTrackStep(catalogbridge.NewTrackLedger(repo), userId, track.ID).Execute(context.Background(), ac, afterStore{}); err != nil {
				t.Fatalf("Execute: %v", err)
			}

			var got Evidence
			if err := json.Unmarshal(storedTrack(t, repo, track).AcquisitionEvidence(), &got); err != nil {
				t.Fatalf("evidence not stored as JSON: %v", err)
			}
			if got.SourceKind != tc.wantKind || got.Grade != tc.wantGrade || got.PipelineGeneration != grade.PipelineGeneration {
				t.Errorf("source_kind=%q grade=%q generation=%d, want %q %q %d", got.SourceKind, got.Grade, got.PipelineGeneration, tc.wantKind, tc.wantGrade, grade.PipelineGeneration)
			}
		})
	}
}

func TestUpdateTrackStep_Execute_ADoubtedReferenceNeverGradesA(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	repo := newFakeTrackRepository()
	track := seedTrackInRepo(t, repo, userId, nil)
	ac := &AcquisitionContext{
		AudioRef: drinkingAudio,
		Selected: &ports.AudioCandidate{Title: "Song", URL: drinkingSource, Kind: ports.KindArtTrack},
		Verdict:  AudioVerdict{Kind: VerdictHard},
		Identity: ports.RecordingIdentity{ReferenceDoubted: true},
	}

	if _, err := NewUpdateTrackStep(catalogbridge.NewTrackLedger(repo), userId, track.ID).Execute(context.Background(), ac, afterStore{}); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var got Evidence
	if err := json.Unmarshal(storedTrack(t, repo, track).AcquisitionEvidence(), &got); err != nil {
		t.Fatalf("evidence not stored as JSON: %v", err)
	}
	if got.Grade != grade.B {
		t.Errorf("grade = %q, want B", got.Grade)
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

const (
	drinkingAudio  = "user/bran-van-3000/drinking-in-la.opus"
	drinkingSource = "https://www.youtube.com/watch?v=-_5yNLsZ5Pk"
)

func TestUpdateTrackStep_AnUncomparedLengthIsNullInTheEvidence(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	repo := newFakeTrackRepository()
	track := seedTrackInRepo(t, repo, userId, nil)
	ac := &AcquisitionContext{
		AudioRef: "user/artist/album/song.mp3",
		Selected: &ports.AudioCandidate{Title: "Song", URL: "https://example.com/v"},
		Evidence: Evidence{Channel: "topic"},
	}

	if _, err := NewUpdateTrackStep(catalogbridge.NewTrackLedger(repo), userId, track.ID).Execute(context.Background(), ac, afterStore{}); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var stored map[string]any
	if err := json.Unmarshal(storedTrack(t, repo, track).AcquisitionEvidence(), &stored); err != nil {
		t.Fatalf("evidence not stored as JSON: %v", err)
	}
	if v, present := stored["duration_delta_seconds"]; !present || v != nil {
		t.Errorf("duration_delta_seconds = %v (present %v), want null", v, present)
	}
}

func TestUpdateTrackStep_EvidenceHasNoUnsetISRCHit(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	repo := newFakeTrackRepository()
	track := seedTrackInRepo(t, repo, userId, nil)
	ac := &AcquisitionContext{
		AudioRef: drinkingAudio,
		Selected: &ports.AudioCandidate{Title: "Song", URL: drinkingSource},
	}

	if _, err := NewUpdateTrackStep(catalogbridge.NewTrackLedger(repo), userId, track.ID).Execute(context.Background(), ac, afterStore{}); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var keys map[string]json.RawMessage
	if err := json.Unmarshal(storedTrack(t, repo, track).AcquisitionEvidence(), &keys); err != nil {
		t.Fatalf("evidence not stored as JSON: %v", err)
	}
	if _, present := keys["isrc_set_hit"]; present {
		t.Errorf("stored evidence carries isrc_set_hit: %v", keys)
	}
}

func TestUpdateTrackStep_Execute_AnUnprobedFetchedFileGetsNoLengthRescue(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	repo := newFakeTrackRepository()
	track := seedTrackInRepo(t, repo, userId, markReadyWith("u/a/b/old.opus"))
	ac := &AcquisitionContext{
		AudioRef: "u/a/b/new.opus",
		Selected: &ports.AudioCandidate{Title: "Song", URL: drinkingSource, Kind: ports.KindUpload, Duration: 285},
		Verdict:  AudioVerdict{Kind: VerdictHard},
		Evidence: Evidence{Channel: ranking.ChannelOther},
		Replace: ReplaceState{
			PreservedRef: "u/a/b/old.opus",
			Gate:         upgrade.Gate{Current: grade.B, CatalogSeconds: 285, StoredSeconds: 231},
		},
	}

	_, err := NewUpdateTrackStep(catalogbridge.NewTrackLedger(repo), userId, track.ID).Execute(context.Background(), ac, afterStore{})
	if !errors.Is(err, upgrade.ErrNotAnUpgrade) {
		t.Fatalf("Execute = %v, want ErrNotAnUpgrade: the source's claimed 285 s was never measured", err)
	}
}

func TestUpdateTrackStep_PassesTheRelabelThroughToTheCompletion(t *testing.T) {
	cases := []struct {
		name    string
		relabel *ports.RecordingLabel
	}{
		{"the re-recording pass set a label", func() *ports.RecordingLabel { label := goldenRerecording(); return &label }()},
		{"no re-recording pass ran", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			userId := shared.NewUserId(uuid.New())
			track, err := domain.NewTrack(userId, "It's Only Love", "Bryan Adams", "Reckless")
			if err != nil {
				t.Fatalf("new track: %v", err)
			}
			repo := newFakeTrackRepository()
			repo.tracks[track.ID.String()+":"+userId.String()] = track
			ledger := &completionRecordingLedger{TrackLedger: catalogbridge.NewTrackLedger(repo)}
			step := NewUpdateTrackStep(ledger, userId, track.ID)

			if _, err := step.Execute(context.Background(), &AcquisitionContext{AudioRef: "u/a/b/c.m4a", Relabel: tc.relabel}, afterStore{}); err != nil {
				t.Fatalf("Execute: %v", err)
			}

			if len(ledger.completed) != 1 || ledger.completed[0].Relabel != tc.relabel {
				t.Errorf("completions = %+v, want one carrying relabel %v", ledger.completed, tc.relabel)
			}
		})
	}
}
