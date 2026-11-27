package downloadfromlibrary_test

import (
	"altune/go-api/internal/app/bridges/downloadfromlibrary"
	"altune/go-api/internal/download"
	"altune/go-api/internal/library"
	"altune/go-api/internal/shared/dbtest"
	sharedids "altune/go-api/internal/shared/ids"
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type readyTrackSpec struct {
	evidence  string
	state     string
	sourceURL *string
}

func newInPlaceRecorder(pool *pgxpool.Pool) *downloadfromlibrary.InPlaceRecorder {
	return downloadfromlibrary.NewInPlaceRecorder(library.NewPostgresTrackRepository(pool, library.NoopDBCallMetrics()), download.NewPostgresUpgradeStore(pool))
}

func insertUpgradableTrack(t *testing.T, pool *pgxpool.Pool, spec readyTrackSpec) sharedids.TrackId {
	t.Helper()
	id := sharedids.TrackIdFromUUID(uuid.New())
	var evidence, state any
	if spec.evidence != "" {
		evidence = spec.evidence
		state = "{}"
	}
	if spec.state != "" {
		state = spec.state
	}
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO tracks (id, user_id, title, artist, album, dedup_key, acquisition_status, audio_ref, audio_source_url, acquisition_evidence)
		 VALUES ($1, $2, 'Blinding Lights', 'The Weeknd', 'After Hours', $3, 'ready', $4, $5, $6::jsonb)`,
		id.UUID(), uuid.New(), uuid.NewString(), "u/"+id.String()+".mp3", spec.sourceURL, evidence); err != nil {
		t.Fatalf("insert ready track: %v", err)
	}
	if state != nil {
		if _, err := pool.Exec(context.Background(),
			`INSERT INTO acquisition_upgrade_state (track_id, evidence) VALUES ($1, $2::jsonb)
			 ON CONFLICT (track_id) DO UPDATE SET evidence = EXCLUDED.evidence`, id.UUID(), state); err != nil {
			t.Fatalf("insert upgrade state: %v", err)
		}
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM tracks WHERE id = $1`, id.UUID())
	})
	return id
}

func sourceURL(id string) *string {
	url := "https://www.youtube.com/watch?v=" + id
	return &url
}

func evidenceOf(g string) string { return fmt.Sprintf(`{"grade":%q}`, g) }

type recordedEvidence struct {
	Grade      string
	Verdict    string
	Basis      string
	Score      float64
	Generation int
	Mismatch   bool
	Channel    string
}

func readEvidence(t *testing.T, pool *pgxpool.Pool, id sharedids.TrackId) recordedEvidence {
	t.Helper()
	var evidence recordedEvidence
	err := pool.QueryRow(context.Background(), `
		SELECT COALESCE(t.acquisition_evidence->>'grade', ''),
			COALESCE(t.acquisition_evidence->>'verdict', ''),
			COALESCE(s.evidence->>'in_place_basis', ''),
			COALESCE((t.acquisition_evidence->>'acoustid_score')::float8, 0),
			COALESCE((s.evidence->>'in_place_generation')::int, 0),
			COALESCE((t.acquisition_evidence->>'user_upload_mismatch')::boolean, false),
			COALESCE(t.acquisition_evidence->>'channel', '')
		FROM tracks t LEFT JOIN acquisition_upgrade_state s ON s.track_id = t.id WHERE t.id = $1`, id.UUID()).
		Scan(&evidence.Grade, &evidence.Verdict, &evidence.Basis, &evidence.Score, &evidence.Generation, &evidence.Mismatch, &evidence.Channel)
	if err != nil {
		t.Fatalf("read evidence: %v", err)
	}
	return evidence
}

func TestInPlaceRecorder_RecordInPlaceWritesTheVerdictAndGradeAndKeepsTheRestOfTheEvidence(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := newPool(t)
	recorder := newInPlaceRecorder(pool)
	id := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: `{"grade":"C","verdict":"unknown","channel":"topic"}`, sourceURL: sourceURL("art")})

	recorded, err := recorder.RecordInPlace(context.Background(), id, download.InPlaceRecord{
		AudioRef: "u/" + id.String() + ".mp3", Generation: 1, Verdict: "hard", Basis: "acoustid", Score: 0.97, Grade: "A",
	})

	if err != nil || !recorded {
		t.Fatalf("RecordInPlace = %v, %v, want it recorded", recorded, err)
	}
	want := recordedEvidence{Grade: "A", Verdict: "hard", Basis: "acoustid", Score: 0.97, Generation: 1, Channel: "topic"}
	if got := readEvidence(t, pool, id); got != want {
		t.Errorf("evidence = %+v, want %+v", got, want)
	}
}

func TestInPlaceRecorder_RecordInPlaceWithNoGradeOrVerdictOnlyStampsTheGeneration(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := newPool(t)
	recorder := newInPlaceRecorder(pool)
	id := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: `{"grade":"C","verdict":"unknown"}`, sourceURL: sourceURL("art")})

	if _, err := recorder.RecordInPlace(context.Background(), id, download.InPlaceRecord{AudioRef: "u/" + id.String() + ".mp3", Generation: 1}); err != nil {
		t.Fatalf("RecordInPlace: %v", err)
	}

	want := recordedEvidence{Grade: "C", Verdict: "unknown", Generation: 1}
	if got := readEvidence(t, pool, id); got != want {
		t.Errorf("evidence = %+v, want %+v", got, want)
	}
}

func TestInPlaceRecorder_RecordInPlaceFlagsAMismatchedUserUploadWithoutRegrading(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := newPool(t)
	recorder := newInPlaceRecorder(pool)
	id := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("D")})

	if _, err := recorder.RecordInPlace(context.Background(), id, download.InPlaceRecord{
		AudioRef: "u/" + id.String() + ".mp3", Generation: 1, Verdict: "other_version", Basis: "acoustid", Score: 0.9, UserUploadMismatch: true,
	}); err != nil {
		t.Fatalf("RecordInPlace: %v", err)
	}

	want := recordedEvidence{Grade: "D", Verdict: "other_version", Basis: "acoustid", Score: 0.9, Generation: 1, Mismatch: true}
	if got := readEvidence(t, pool, id); got != want {
		t.Errorf("evidence = %+v, want %+v", got, want)
	}
}

func TestInPlaceRecorder_RecordInPlaceNeverLowersTheStoredGrade(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := newPool(t)
	recorder := newInPlaceRecorder(pool)
	id := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("C"), sourceURL: sourceURL("art")})
	ctx := context.Background()
	ref := "u/" + id.String() + ".mp3"

	for _, grade := range []string{"F", "D", "C"} {
		if _, err := recorder.RecordInPlace(ctx, id, download.InPlaceRecord{AudioRef: ref, Generation: 1, Verdict: "other_version", Grade: grade}); err != nil {
			t.Fatalf("RecordInPlace %s: %v", grade, err)
		}
		if got := readEvidence(t, pool, id); got.Grade != "C" || got.Verdict != "other_version" {
			t.Errorf("after %s: evidence = %+v, want grade C kept and the verdict recorded", grade, got)
		}
	}
	if _, err := recorder.RecordInPlace(ctx, id, download.InPlaceRecord{AudioRef: ref, Generation: 1, Verdict: "preview", Basis: "preview", BitErrorRate: 0.06, Grade: "A"}); err != nil {
		t.Fatalf("RecordInPlace A: %v", err)
	}
	var rate float64
	if err := pool.QueryRow(ctx, `SELECT (evidence->>'in_place_bit_error_rate')::float8 FROM acquisition_upgrade_state WHERE track_id = $1`, id.UUID()).Scan(&rate); err != nil {
		t.Fatalf("read bit error rate: %v", err)
	}
	if got := readEvidence(t, pool, id); got.Grade != "A" || got.Verdict != "preview" || rate != 0.06 {
		t.Errorf("evidence = %+v, rate = %v, want the raise to A with the preview verdict and its bit error rate", got, rate)
	}
}

func TestInPlaceRecorder_RecordInPlaceLowersTheGradeOnlyForAContradictionThatStands(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := newPool(t)
	recorder := newInPlaceRecorder(pool)
	id := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("C"), sourceURL: sourceURL("art")})

	recorded, err := recorder.RecordInPlace(context.Background(), id, download.InPlaceRecord{
		AudioRef: "u/" + id.String() + ".mp3", Generation: 1, Verdict: "other_version", Basis: "acoustid", Grade: "F", MayLower: true,
	})

	if err != nil || !recorded {
		t.Fatalf("RecordInPlace = %v, %v, want it recorded", recorded, err)
	}
	if got := readEvidence(t, pool, id); got.Grade != "F" || got.Verdict != "other_version" {
		t.Errorf("evidence = %+v, want the stored grade to tell the truth about the file: F", got)
	}
}

func TestInPlaceRecorder_RecordInPlaceIgnoresAFileThatChangedMeanwhile(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := newPool(t)
	recorder := newInPlaceRecorder(pool)
	id := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("C"), sourceURL: sourceURL("art")})

	recorded, err := recorder.RecordInPlace(context.Background(), id, download.InPlaceRecord{AudioRef: "u/replaced-since.mp3", Generation: 1, Verdict: "hard", Grade: "A"})

	if err != nil || recorded {
		t.Fatalf("RecordInPlace = %v, %v, want a verdict on an old file to be dropped", recorded, err)
	}
	if got := readEvidence(t, pool, id); got.Grade != "C" || got.Generation != 0 {
		t.Errorf("evidence = %+v, want the track untouched", got)
	}
}

func TestInPlaceRecorder_RecordInPlaceNeverReplacesAStoredBestAvailableGrade(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := newPool(t)
	recorder := newInPlaceRecorder(pool)
	id := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("best_available"), sourceURL: sourceURL("leak")})

	if _, err := recorder.RecordInPlace(context.Background(), id, download.InPlaceRecord{
		AudioRef: "u/" + id.String() + ".mp3", Generation: 1, Verdict: "contradicted", Basis: "acoustid", Grade: "F", MayLower: true,
	}); err != nil {
		t.Fatalf("RecordInPlace: %v", err)
	}

	if got := readEvidence(t, pool, id); got.Grade != "best_available" {
		t.Errorf("grade = %q, want best_available kept: it is never replaced", got.Grade)
	}
}

func TestInPlaceRecorder_RecordInPlaceLowersAWrongLengthGradeAToB(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := newPool(t)
	recorder := newInPlaceRecorder(pool)
	id := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("A"), sourceURL: sourceURL("art")})
	ref := "u/" + id.String() + ".mp3"

	if _, err := recorder.RecordInPlace(context.Background(), id, download.InPlaceRecord{AudioRef: ref, Generation: 2, Verdict: "hard", Grade: "B"}); err != nil {
		t.Fatalf("RecordInPlace: %v", err)
	}
	if got := readEvidence(t, pool, id); got.Grade != "A" {
		t.Fatalf("grade = %q, want A kept: weaker evidence alone never lowers", got.Grade)
	}
	if _, err := recorder.RecordInPlace(context.Background(), id, download.InPlaceRecord{AudioRef: ref, Generation: 2, Verdict: "hard", Grade: "B", MayLower: true}); err != nil {
		t.Fatalf("RecordInPlace: %v", err)
	}

	if got := readEvidence(t, pool, id); got.Grade != "B" {
		t.Errorf("grade = %q, want B once the length contradicts the file", got.Grade)
	}
}

func TestInPlaceRecorder_RecordInPlaceClearsTheFailureCountSoANewGenerationStartsFresh(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := newPool(t)
	recorder := newInPlaceRecorder(pool)
	id := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("C"), state: `{"in_place_failures":2}`, sourceURL: sourceURL("art")})

	if _, err := recorder.RecordInPlace(context.Background(), id, download.InPlaceRecord{AudioRef: "u/" + id.String() + ".mp3", Generation: 2, Verdict: "hard", Grade: "A"}); err != nil {
		t.Fatalf("RecordInPlace: %v", err)
	}
	failures, err := recorder.NoteInPlaceFailure(context.Background(), id)

	if err != nil || failures != 1 {
		t.Errorf("failures after a recorded verdict = %d, %v, want 1: the count starts over", failures, err)
	}
}

func TestInPlaceRecorder_RecordInPlaceClearsAStaleAcoustIDScoreOnAPreviewVerdict(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := newPool(t)
	recorder := newInPlaceRecorder(pool)
	id := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: `{"grade":"C","acoustid_score":0.9}`, sourceURL: sourceURL("art")})

	if _, err := recorder.RecordInPlace(context.Background(), id, download.InPlaceRecord{
		AudioRef: "u/" + id.String() + ".mp3", Generation: 2, Verdict: "preview", Basis: "preview", BitErrorRate: 0.06,
	}); err != nil {
		t.Fatalf("RecordInPlace: %v", err)
	}

	if got := readEvidence(t, pool, id); got.Score != 0 || got.Verdict != "preview" {
		t.Errorf("evidence = %+v, want the old score cleared and the preview verdict recorded", got)
	}
}
