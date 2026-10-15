package persistence

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/acquisition/service/grade"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/sharedtest"
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type readyTrackSpec struct {
	evidence  string
	sourceURL *string
}

func insertUpgradableTrack(t *testing.T, pool *pgxpool.Pool, spec readyTrackSpec) shared.TrackId {
	t.Helper()
	id := shared.TrackIdFromUUID(uuid.New())
	var evidence any
	if spec.evidence != "" {
		evidence = spec.evidence
	}
	_, err := pool.Exec(context.Background(),
		`INSERT INTO tracks (id, user_id, title, artist, album, dedup_key, acquisition_status, audio_ref, audio_source_url, acquisition_evidence)
		 VALUES ($1, $2, 'Blinding Lights', 'The Weeknd', 'After Hours', $3, 'ready', $4, $5, $6::jsonb)`,
		id.UUID(), uuid.New(), uuid.NewString(), "u/"+id.String()+".mp3", spec.sourceURL, evidence)
	if err != nil {
		t.Fatalf("insert ready track: %v", err)
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

func selectionOf(limit int) ports.UpgradeSelection {
	return ports.UpgradeSelection{Limit: limit, Generation: grade.PipelineGeneration, RetryAfter: time.Hour, MaxQueued: 10}
}

func candidatesAmong(t *testing.T, store *PgxUpgradeStore, selection ports.UpgradeSelection, mine ...shared.TrackId) []shared.TrackId {
	t.Helper()
	selection.Limit = 1000
	candidates, err := store.Candidates(context.Background(), selection)
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	var ids []shared.TrackId
	for _, c := range candidates {
		if slices.Contains(mine, c.TrackID) {
			ids = append(ids, c.TrackID)
		}
	}
	return ids
}

func TestPgxUpgradeStore_CandidatesAreWorstGradeFirstAndNeverTheGradeA(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxUpgradeStore(pool)

	b := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("B"), sourceURL: sourceURL("bbbb")})
	f := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("F"), sourceURL: sourceURL("ffff")})
	c := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("C"), sourceURL: sourceURL("cccc")})
	d := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("D"), sourceURL: sourceURL("dddd")})
	a := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("A"), sourceURL: sourceURL("aaaa")})

	got := candidatesAmong(t, store, selectionOf(10), a, b, c, d, f)

	want := []shared.TrackId{f, d, c, b}
	if !slices.Equal(got, want) {
		t.Errorf("candidates = %v, want worst first F, D, C, B and no grade A: %v", got, want)
	}
}

func TestPgxUpgradeStore_ATrackWithNoGradeIsPickedAsGradeD(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxUpgradeStore(pool)

	ungraded := insertUpgradableTrack(t, pool, readyTrackSpec{sourceURL: sourceURL("none")})

	got := candidatesAmong(t, store, selectionOf(10), ungraded)

	if !slices.Equal(got, []shared.TrackId{ungraded}) {
		t.Errorf("candidates = %v, want the ungraded track", got)
	}
}

func TestPgxUpgradeStore_AUserUploadIsNeverACandidate(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxUpgradeStore(pool)

	noSource := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("F"), sourceURL: nil})
	empty := ""
	emptySource := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("F"), sourceURL: &empty})
	fromSource := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("F"), sourceURL: sourceURL("real")})

	got := candidatesAmong(t, store, selectionOf(10), noSource, emptySource, fromSource)

	if !slices.Equal(got, []shared.TrackId{fromSource}) {
		t.Errorf("candidates = %v, want only the track that came from a source", got)
	}
}

func TestPgxUpgradeStore_AFileThatIsNotReadyIsNeverACandidate(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxUpgradeStore(pool)

	failed := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("F"), sourceURL: sourceURL("gone")})
	if _, err := pool.Exec(context.Background(), `UPDATE tracks SET acquisition_status = 'failed' WHERE id = $1`, failed.UUID()); err != nil {
		t.Fatalf("fail track: %v", err)
	}

	if got := candidatesAmong(t, store, selectionOf(10), failed); len(got) != 0 {
		t.Errorf("candidates = %v, want none for a track with no playable file", got)
	}
}

func TestPgxUpgradeStore_ATrackGivenUpOnWaitsForTheNextGeneration(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxUpgradeStore(pool)
	id := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("D"), sourceURL: sourceURL("stuck")})

	if err := store.GiveUp(context.Background(), id, grade.PipelineGeneration, "the extended mix exists only on a promo channel"); err != nil {
		t.Fatalf("GiveUp: %v", err)
	}

	if got := candidatesAmong(t, store, selectionOf(10), id); len(got) != 0 {
		t.Errorf("candidates in the same generation = %v, want none", got)
	}
	next := selectionOf(10)
	next.Generation = grade.PipelineGeneration + 1
	if got := candidatesAmong(t, store, next, id); !slices.Equal(got, []shared.TrackId{id}) {
		t.Errorf("candidates after the generation bump = %v, want the track again", got)
	}
	var reason string
	if err := pool.QueryRow(context.Background(),
		`SELECT acquisition_evidence->>'best_available_reason' FROM tracks WHERE id = $1`, id.UUID()).Scan(&reason); err != nil {
		t.Fatalf("read reason: %v", err)
	}
	if reason != "the extended mix exists only on a promo channel" {
		t.Errorf("best_available_reason = %q, want the written reason", reason)
	}
}

func TestPgxUpgradeStore_ATrackTriedRecentlyWaitsBeforeItsNextTry(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxUpgradeStore(pool)
	tried := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("F"), sourceURL: sourceURL("tried")})
	fresh := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("D"), sourceURL: sourceURL("fresh")})

	if _, err := store.NoteAttempt(context.Background(), tried, ports.UpgradeAttempt{}); err != nil {
		t.Fatalf("NoteAttempt: %v", err)
	}

	if got := candidatesAmong(t, store, selectionOf(10), tried, fresh); !slices.Equal(got, []shared.TrackId{fresh}) {
		t.Errorf("candidates = %v, want the untried track while the tried one waits", got)
	}
}

func TestPgxUpgradeStore_NoCandidatesWhileTheUpgradeQueueIsFull(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxUpgradeStore(pool)
	queued := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("F"), sourceURL: sourceURL("queued")})
	waiting := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("D"), sourceURL: sourceURL("waiting")})
	if err := NewPgxJobQueue(pool).Enqueue(context.Background(), queued, ports.JobKindUpgrade, 0); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	full := selectionOf(10)
	full.MaxQueued = 1

	if got := candidatesAmong(t, store, full, queued, waiting); len(got) != 0 {
		t.Errorf("candidates = %v, want none while the queue holds its maximum", got)
	}
}

func TestPgxUpgradeStore_NoteAttemptCountsTriesAndRemembersTheRejectedSource(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxUpgradeStore(pool)
	id := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("D"), sourceURL: sourceURL("current")})
	ctx := context.Background()

	first, err := store.NoteAttempt(ctx, id, ports.UpgradeAttempt{RejectedSourceKey: "youtube:aaa", CountsAsTry: true})
	if err != nil {
		t.Fatalf("NoteAttempt: %v", err)
	}
	second, err := store.NoteAttempt(ctx, id, ports.UpgradeAttempt{RejectedSourceKey: "youtube:aaa", CountsAsTry: true})
	if err != nil {
		t.Fatalf("NoteAttempt: %v", err)
	}
	uncounted, err := store.NoteAttempt(ctx, id, ports.UpgradeAttempt{RejectedSourceKey: "youtube:bbb"})
	if err != nil {
		t.Fatalf("NoteAttempt: %v", err)
	}

	if first != 1 || second != 2 || uncounted != 2 {
		t.Errorf("tries = %d, %d, %d, want 1, 2, 2 (an uncounted attempt leaves the count alone)", first, second, uncounted)
	}
	var keys []string
	var grade string
	if err := pool.QueryRow(ctx, `SELECT rejected_source_keys, acquisition_evidence->>'grade' FROM tracks WHERE id = $1`, id.UUID()).Scan(&keys, &grade); err != nil {
		t.Fatalf("read track: %v", err)
	}
	if !slices.Equal(keys, []string{"youtube:aaa", "youtube:bbb"}) {
		t.Errorf("rejected_source_keys = %v, want each source once", keys)
	}
	if grade != "D" {
		t.Errorf("grade = %q, want the stored grade kept", grade)
	}
}

func TestPgxUpgradeStore_RejectedSourceKeysKeepTheNewestTwentyFive(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxUpgradeStore(pool)
	id := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("D"), sourceURL: sourceURL("current")})

	for i := range 27 {
		attempt := ports.UpgradeAttempt{RejectedSourceKey: fmt.Sprintf("youtube:%02d", i)}
		if _, err := store.NoteAttempt(context.Background(), id, attempt); err != nil {
			t.Fatalf("NoteAttempt: %v", err)
		}
	}

	var keys []string
	if err := pool.QueryRow(context.Background(), `SELECT rejected_source_keys FROM tracks WHERE id = $1`, id.UUID()).Scan(&keys); err != nil {
		t.Fatalf("read keys: %v", err)
	}
	if len(keys) != 25 || keys[0] != "youtube:02" || keys[24] != "youtube:26" {
		t.Errorf("rejected_source_keys = %v, want the newest 25 (02 to 26)", keys)
	}
}
