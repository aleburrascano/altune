package upgrade_test

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/acquisition/service/grade"
	"altune/go-api/internal/acquisition/service/upgrade"
	"altune/go-api/internal/shared"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
)

const verifyPerHour = 3600

type fixedChecker struct {
	finding upgrade.Finding
	err     error
	calls   int
}

func (c *fixedChecker) CheckInPlace(context.Context, *ports.TrackSnapshot) (upgrade.Finding, error) {
	c.calls++
	return c.finding, c.err
}

type recordingInPlaceStore struct {
	mu      sync.Mutex
	records []ports.InPlaceRecord
}

func (s *recordingInPlaceStore) RecordInPlace(_ context.Context, _ shared.TrackId, record ports.InPlaceRecord) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, record)
	return true, nil
}

var (
	hardMatch  = upgrade.Finding{Verdict: "hard", Fingerprint: grade.Matches, Basis: "acoustid", Score: 0.97}
	otherTake  = upgrade.Finding{Verdict: "other_version", Fingerprint: grade.Contradicts, Basis: "acoustid", Score: 0.93}
	wrongSong  = upgrade.Finding{Verdict: "different_song", Fingerprint: grade.Unsupported, Basis: "acoustid", Score: 0.9}
	noAnswer   = upgrade.Finding{}
	softMatch  = upgrade.Finding{Verdict: "soft", Fingerprint: grade.Approximates, Basis: "acoustid", Score: 0.9}
	previewHit = upgrade.Finding{Verdict: "hard", Fingerprint: grade.Matches, Basis: "preview"}
)

func (w *world) storedEvidence(evidence string) {
	key := rowKey(w.trackId, w.userId)
	row := w.tracks.rows[key]
	row.EvidenceJSON = json.RawMessage(evidence)
	w.tracks.rows[key] = row
}

func (w *world) checkedRunner(cfg upgrade.Config, checker upgrade.InPlaceChecker, recorder ports.InPlaceStore) *upgrade.Runner {
	return w.runner(cfg).WithInPlaceCheck(checker, recorder, verifyPerHour)
}

func evidenceFor(g grade.Grade, kind grade.SourceKind) string {
	return fmt.Sprintf(`{"grade":%q,"source_kind":%q,"pipeline_generation":1}`, g, kind)
}

func TestRunner_AnArtTrackGradedCWithAMatchingFingerprintBecomesAWithNoDownload(t *testing.T) {
	w := seed(t, grade.C)
	w.storedEvidence(evidenceFor(grade.C, grade.ArtTrack))
	recorder := &recordingInPlaceStore{}

	err := w.checkedRunner(upgrade.Config{MaxTries: 3}, &fixedChecker{finding: hardMatch}, recorder).ExecuteUpgrade(context.Background(), w.userId, w.trackId)
	if err != nil {
		t.Fatalf("ExecuteUpgrade: %v", err)
	}
	want := ports.InPlaceRecord{AudioRef: originalRef, Generation: grade.PipelineGeneration, Verdict: "hard", Basis: "acoustid", Score: 0.97, Grade: "A"}
	if len(recorder.records) != 1 || recorder.records[0] != want {
		t.Errorf("records = %+v, want exactly %+v", recorder.records, want)
	}
	if w.source.finds != 0 {
		t.Errorf("sources searched %d times, want 0: the file was proven in place", w.source.finds)
	}
	if row := w.committed(); *row.AudioRef != originalRef || len(w.audio.objects) != 1 {
		t.Errorf("audio_ref = %v, objects = %v, want the file untouched", row.AudioRef, w.audio.objects)
	}
}

func TestRunner_APreviewMatchProvesTheFileAsWellAsAFingerprint(t *testing.T) {
	w := seed(t, grade.D)
	w.storedEvidence(evidenceFor(grade.D, grade.ArtTrack))
	recorder := &recordingInPlaceStore{}

	if err := w.checkedRunner(upgrade.Config{MaxTries: 3}, &fixedChecker{finding: previewHit}, recorder).ExecuteUpgrade(context.Background(), w.userId, w.trackId); err != nil {
		t.Fatalf("ExecuteUpgrade: %v", err)
	}

	if len(recorder.records) != 1 || recorder.records[0].Grade != "A" || recorder.records[0].Basis != "preview" {
		t.Errorf("records = %+v, want grade A on the preview basis", recorder.records)
	}
	if w.source.finds != 0 {
		t.Errorf("sources searched %d times, want 0", w.source.finds)
	}
}

func TestRunner_AMismatchRecordsTheVerdictThenFallsThroughToReacquire(t *testing.T) {
	w := seed(t, grade.C)
	w.storedEvidence(evidenceFor(grade.C, grade.ArtTrack))
	recorder := &recordingInPlaceStore{}

	err := w.checkedRunner(upgrade.Config{MaxTries: 3}, &fixedChecker{finding: otherTake}, recorder).ExecuteUpgrade(context.Background(), w.userId, w.trackId)
	if err != nil {
		t.Fatalf("ExecuteUpgrade: %v", err)
	}
	if len(recorder.records) != 1 || recorder.records[0].Grade != "F" || recorder.records[0].Verdict != "other_version" {
		t.Errorf("records = %+v, want the F verdict recorded", recorder.records)
	}
	if w.source.finds != 1 {
		t.Errorf("sources searched %d times, want 1: a failed check re-acquires", w.source.finds)
	}
	if row := w.committed(); *row.AudioRef == originalRef {
		t.Error("the better file should replace the one the fingerprint rejected")
	}
}

func TestRunner_AFingerprintThatNeverAnswersFallsThroughToReacquireWithoutChangingTheGrade(t *testing.T) {
	w := seed(t, grade.C)
	w.storedEvidence(evidenceFor(grade.C, grade.ArtTrack))
	recorder := &recordingInPlaceStore{}

	if err := w.checkedRunner(upgrade.Config{MaxTries: 3}, &fixedChecker{finding: noAnswer}, recorder).ExecuteUpgrade(context.Background(), w.userId, w.trackId); err != nil && !errors.Is(err, upgrade.ErrKept) {
		t.Fatalf("ExecuteUpgrade: %v", err)
	}

	if len(recorder.records) != 1 || recorder.records[0].Grade != "" || recorder.records[0].Generation != grade.PipelineGeneration {
		t.Errorf("records = %+v, want the generation stamped and the grade left alone", recorder.records)
	}
	if w.source.finds != 1 {
		t.Errorf("sources searched %d times, want 1", w.source.finds)
	}
}

func TestRunner_AMatchOnAnUnofficialSourceIsBAndStillLooksForAnOfficialOne(t *testing.T) {
	w := seed(t, grade.C)
	w.storedEvidence(evidenceFor(grade.C, grade.FanUpload))
	recorder := &recordingInPlaceStore{}

	if err := w.checkedRunner(upgrade.Config{MaxTries: 3}, &fixedChecker{finding: hardMatch}, recorder).ExecuteUpgrade(context.Background(), w.userId, w.trackId); err != nil && !errors.Is(err, upgrade.ErrKept) {
		t.Fatalf("ExecuteUpgrade: %v", err)
	}

	if len(recorder.records) != 1 || recorder.records[0].Grade != "B" {
		t.Errorf("records = %+v, want B for a proven fan upload", recorder.records)
	}
	if w.source.finds != 1 {
		t.Errorf("sources searched %d times, want 1: B is still below A", w.source.finds)
	}
}

func TestRunner_ATrackCheckedThisGenerationIsNotCheckedAgain(t *testing.T) {
	w := seed(t, grade.C)
	w.storedEvidence(`{"grade":"C","source_kind":"art_track","in_place_generation":1}`)
	checker := &fixedChecker{finding: hardMatch}

	if err := w.checkedRunner(upgrade.Config{MaxTries: 3}, checker, &recordingInPlaceStore{}).ExecuteUpgrade(context.Background(), w.userId, w.trackId); err != nil && !errors.Is(err, upgrade.ErrKept) {
		t.Fatalf("ExecuteUpgrade: %v", err)
	}

	if checker.calls != 0 || w.source.finds != 1 {
		t.Errorf("checks = %d, searches = %d, want 0 checks and a straight re-acquire", checker.calls, w.source.finds)
	}
}

func TestRunner_AUserUploadThatMatchesIsGradedAUserAndNeverReplaced(t *testing.T) {
	w := seed(t, grade.D)
	w.asUserUpload()
	recorder := &recordingInPlaceStore{}

	if err := w.checkedRunner(upgrade.Config{MaxTries: 3}, &fixedChecker{finding: hardMatch}, recorder).ExecuteUpgrade(context.Background(), w.userId, w.trackId); err != nil {
		t.Fatalf("ExecuteUpgrade: %v", err)
	}

	if len(recorder.records) != 1 || recorder.records[0].Grade != "A_user" || recorder.records[0].UserUploadMismatch {
		t.Errorf("records = %+v, want grade A_user", recorder.records)
	}
	if w.source.finds != 0 || *w.committed().AudioRef != originalRef || len(w.audio.objects) != 1 {
		t.Errorf("searches = %d, objects = %v, want the user's file untouched", w.source.finds, w.audio.objects)
	}
}

func TestRunner_AUserUploadThatDoesNotMatchIsFlaggedAndNeverReplaced(t *testing.T) {
	for name, finding := range map[string]upgrade.Finding{"another version": otherTake, "another song": wrongSong} {
		t.Run(name, func(t *testing.T) {
			w := seed(t, grade.D)
			w.asUserUpload()
			recorder := &recordingInPlaceStore{}

			if err := w.checkedRunner(upgrade.Config{MaxTries: 3}, &fixedChecker{finding: finding}, recorder).ExecuteUpgrade(context.Background(), w.userId, w.trackId); err != nil {
				t.Fatalf("ExecuteUpgrade: %v", err)
			}

			if len(recorder.records) != 1 || !recorder.records[0].UserUploadMismatch || recorder.records[0].Grade != "" {
				t.Errorf("records = %+v, want the mismatch flagged and the grade left alone", recorder.records)
			}
			if w.source.finds != 0 || *w.committed().AudioRef != originalRef || len(w.audio.objects) != 1 {
				t.Errorf("searches = %d, objects = %v, want the user's file untouched", w.source.finds, w.audio.objects)
			}
		})
	}
}

func TestRunner_AUserUploadWithNoAnswerOrOnlyASoftOneIsNeitherGradedNorFlagged(t *testing.T) {
	for name, finding := range map[string]upgrade.Finding{"no answer": noAnswer, "soft": softMatch} {
		t.Run(name, func(t *testing.T) {
			w := seed(t, grade.D)
			w.asUserUpload()
			recorder := &recordingInPlaceStore{}

			if err := w.checkedRunner(upgrade.Config{MaxTries: 3}, &fixedChecker{finding: finding}, recorder).ExecuteUpgrade(context.Background(), w.userId, w.trackId); err != nil {
				t.Fatalf("ExecuteUpgrade: %v", err)
			}

			if len(recorder.records) != 1 || recorder.records[0].Grade != "" || recorder.records[0].UserUploadMismatch {
				t.Errorf("records = %+v, want only the generation stamped", recorder.records)
			}
			if w.source.finds != 0 {
				t.Errorf("searches = %d, want 0 for a user's file", w.source.finds)
			}
		})
	}
}

func TestRunner_ADryRunChecksButWritesNoVerdict(t *testing.T) {
	w := seed(t, grade.C)
	w.storedEvidence(evidenceFor(grade.C, grade.ArtTrack))
	recorder := &recordingInPlaceStore{}
	checker := &fixedChecker{finding: hardMatch}

	if err := w.checkedRunner(upgrade.Config{MaxTries: 3, DryRun: true}, checker, recorder).ExecuteUpgrade(context.Background(), w.userId, w.trackId); err != nil {
		t.Fatalf("ExecuteUpgrade: %v", err)
	}

	if checker.calls != 1 || len(recorder.records) != 0 {
		t.Errorf("checks = %d, records = %+v, want the check run and nothing recorded", checker.calls, recorder.records)
	}
	if w.source.finds != 0 || *w.committed().AudioRef != originalRef {
		t.Errorf("searches = %d, audio_ref = %v, want a dry run proven in place to stop there", w.source.finds, w.committed().AudioRef)
	}
}

func TestRunner_ACheckThatFailsRecordsNothingAndReportsTheError(t *testing.T) {
	w := seed(t, grade.C)
	w.storedEvidence(evidenceFor(grade.C, grade.ArtTrack))
	recorder := &recordingInPlaceStore{}
	acoustIDDown := errors.New("acoustid unreachable")

	err := w.checkedRunner(upgrade.Config{MaxTries: 3}, &fixedChecker{err: acoustIDDown}, recorder).ExecuteUpgrade(context.Background(), w.userId, w.trackId)

	if !errors.Is(err, acoustIDDown) {
		t.Errorf("ExecuteUpgrade = %v, want the outage reported", err)
	}
	if len(recorder.records) != 0 || w.source.finds != 0 {
		t.Errorf("records = %+v, searches = %d, want nothing recorded and no download on a failed check", recorder.records, w.source.finds)
	}
	if fmt.Sprint(w.upgrades.attempts) != fmt.Sprint([]ports.UpgradeAttempt{{}}) {
		t.Errorf("attempts = %v, want one uncounted attempt so the track waits its turn", w.upgrades.attempts)
	}
}
