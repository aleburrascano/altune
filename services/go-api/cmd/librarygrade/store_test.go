package main

import (
	"altune/go-api/internal/acquisition/adapters/ytmsearch"
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testStore(t *testing.T) store {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set, skipping integration test")
	}
	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return store{pool: pool}
}

func seedReadyTrack(t *testing.T, s store, userID string, sourceURL, provenance, evidence *string) string {
	t.Helper()
	id := uuid.NewString()
	_, err := s.pool.Exec(context.Background(),
		`INSERT INTO tracks (id, user_id, title, artist, dedup_key, acquisition_status, audio_ref, audio_source_url, acquisition_provenance, acquisition_evidence, version)
		 VALUES ($1, $2, 't', 'Daft Punk', $6, 'ready', 'u/a.opus', $3, $4, $5::jsonb, 4)`,
		id, userID, sourceURL, provenance, evidence, id)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	t.Cleanup(func() { _, _ = s.pool.Exec(context.Background(), "DELETE FROM tracks WHERE id = $1", id) })
	return id
}

func text(value string) *string { return &value }

type evidenceSnapshot struct {
	evidence *string
	version  int
}

func snapshot(t *testing.T, s store, id string) evidenceSnapshot {
	t.Helper()
	var got evidenceSnapshot
	if err := s.pool.QueryRow(context.Background(), "SELECT acquisition_evidence::text, version FROM tracks WHERE id = $1", id).Scan(&got.evidence, &got.version); err != nil {
		t.Fatal(err)
	}
	return got
}

func backfill(t *testing.T, s store, userID string, write bool) (uuid.UUID, string, error) {
	t.Helper()
	runID := uuid.New()
	var out bytes.Buffer
	lookup := &fakeLookup{watch: ytmsearch.Watch{Type: ytmsearch.VideoTypeATV}}
	err := runBackfill(context.Background(), s, lookup, options{apply: write, userID: userID}, runID, &out)
	return runID, out.String(), err
}

func TestRunBackfill_DryRunWritesNothingAndCountsPerGrade(t *testing.T) {
	s, user := testStore(t), uuid.NewString()
	id := seedReadyTrack(t, s, user, text(youtubeURL), text("verified"), text(`{"verdict":"hard","channel":"other"}`))

	_, out, err := backfill(t, s, user, false)

	got := snapshot(t, s, id)
	if err != nil || got.version != 4 || strings.Contains(*got.evidence, "source_kind") {
		t.Fatalf("err=%v got=%+v", err, got)
	}
	if !strings.Contains(out, "DRY RUN") || !strings.Contains(out, "would apply:") || !strings.Contains(out, "grade A:") {
		t.Fatalf("out=%s", out)
	}
}

func TestRunBackfill_ApplyKeepsExistingEvidenceAndAddsKindGenerationGradeAndRun(t *testing.T) {
	s, user := testStore(t), uuid.NewString()
	id := seedReadyTrack(t, s, user, text(youtubeURL), text("verified"), text(`{"verdict":"hard","channel":"other"}`))

	runID, out, err := backfill(t, s, user, true)

	got := snapshot(t, s, id)
	for _, want := range []string{`"channel": "other"`, `"source_kind": "art_track"`, `"grade": "A"`, `"pipeline_generation": 0`, `"backfill_run": "` + runID.String() + `"`} {
		if err != nil || !strings.Contains(*got.evidence, want) {
			t.Fatalf("missing %s: err=%v evidence=%s out=%s", want, err, *got.evidence, out)
		}
	}
	if got.version != 5 {
		t.Fatalf("version = %d, want 5", got.version)
	}
}

func TestRunBackfill_GivesATrackWithNoEvidenceAndNoSourceTheUserUploadKind(t *testing.T) {
	s, user := testStore(t), uuid.NewString()
	id := seedReadyTrack(t, s, user, nil, nil, nil)

	runID, _, err := backfill(t, s, user, true)

	got := snapshot(t, s, id)
	if err != nil || got.evidence == nil || !strings.Contains(*got.evidence, `"source_kind": "user_upload"`) || !strings.Contains(*got.evidence, `"grade": "D"`) {
		t.Fatalf("err=%v got=%+v run=%s", err, got, runID)
	}
}

func TestRunBackfill_NeverTouchesATrackThatAlreadyHasASourceKind(t *testing.T) {
	s, user := testStore(t), uuid.NewString()
	id := seedReadyTrack(t, s, user, text(youtubeURL), text("verified"), text(`{"verdict":"hard","source_kind":"label_upload","grade":"A"}`))

	_, _, err := backfill(t, s, user, true)

	if got := snapshot(t, s, id); err != nil || got.version != 4 || strings.Contains(*got.evidence, "backfill_run") {
		t.Fatalf("err=%v got=%+v", err, got)
	}
}

func TestRunUndo_RestoresTheEvidenceTheRunFound(t *testing.T) {
	s, user := testStore(t), uuid.NewString()
	withEvidence := seedReadyTrack(t, s, user, text(youtubeURL), text("verified"), text(`{"verdict":"hard"}`))
	withoutEvidence := seedReadyTrack(t, s, user, nil, nil, nil)
	runID, _, err := backfill(t, s, user, true)
	if err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	undoErr := runUndo(context.Background(), s, runID, &out)

	kept, cleared := snapshot(t, s, withEvidence), snapshot(t, s, withoutEvidence)
	if undoErr != nil || *kept.evidence != `{"verdict": "hard"}` || cleared.evidence != nil {
		t.Fatalf("err=%v kept=%+v cleared=%+v", undoErr, kept, cleared)
	}
}

func TestRunUndo_FailsForARunThatWroteNothing(t *testing.T) {
	s := testStore(t)

	if err := runUndo(context.Background(), s, uuid.New(), &bytes.Buffer{}); err == nil {
		t.Fatal("expected an error")
	}
}
