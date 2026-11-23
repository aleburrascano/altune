package service

import (
	"altune/go-api/internal/acquisition/internal/adapters/catalogbridge"
	"altune/go-api/internal/acquisition/internal/core/grade"
	"altune/go-api/internal/acquisition/internal/core/ports"
	"altune/go-api/internal/acquisition/internal/core/ranking"
	shared "altune/go-api/internal/shared/kernel"
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

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
