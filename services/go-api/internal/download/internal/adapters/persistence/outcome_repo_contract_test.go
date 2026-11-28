package persistence_test

import (
	"altune/go-api/internal/download/internal/adapters/persistence"
	"altune/go-api/internal/download/internal/contracttest"
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/shared/dbtest"
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

type readableOutcomes struct {
	*persistence.PostgresOutcomeStore
	pool *pgxpool.Pool
}

const readOutcomesSQL = `
SELECT track_id, outcome, reason, elapsed_ms, stage_ms,
	source_host, source_format_id, source_codec, COALESCE(source_kbps, 0), COALESCE(stored_bytes, 0), stored_codec,
	job_kind, COALESCE(run, 0), provenance, COALESCE(confidence, 0), source_key,
	COALESCE(target_seconds, 0), COALESCE(measured_seconds, 0)
FROM acquisition_outcomes WHERE track_id = $1 ORDER BY id`

func (r readableOutcomes) Recorded(t *testing.T, trackID string) []domain.AcquisitionOutcome {
	t.Helper()
	rows, err := r.pool.Query(context.Background(), readOutcomesSQL, trackID)
	if err != nil {
		t.Fatalf("query acquisition_outcomes: %v", err)
	}
	defer rows.Close()
	var got []domain.AcquisitionOutcome
	for rows.Next() {
		var o domain.AcquisitionOutcome
		var outcome, kind string
		var stageMs []byte
		if err := rows.Scan(&o.TrackID, &outcome, &o.Reason, &o.ElapsedMs, &stageMs,
			&o.SourceHost, &o.Source.ID, &o.Source.Codec, &o.Source.Kbps, &o.Stored.Bytes, &o.Stored.Codec,
			&kind, &o.Run, &o.Provenance, &o.Confidence, &o.SourceKey, &o.TargetSeconds, &o.MeasuredSeconds); err != nil {
			t.Fatalf("scan acquisition_outcomes row: %v", err)
		}
		o.Outcome, o.Kind = domain.JobState(outcome), domain.JobKind(kind)
		if len(stageMs) > 0 {
			if err := json.Unmarshal(stageMs, &o.StageMs); err != nil {
				t.Fatalf("stage_ms %s: %v", stageMs, err)
			}
		}
		got = append(got, o)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate acquisition_outcomes rows: %v", err)
	}
	return got
}

func TestPostgresOutcomeStore_KeepsTheOutcomeRepoContract(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := dbtest.Pool(t)
	dbtest.ExclusiveAcquisitionQueue(t, pool)
	contracttest.OutcomeRepoContract(t, func(*testing.T) contracttest.OutcomeRepoUnderTest {
		return readableOutcomes{PostgresOutcomeStore: persistence.NewPostgresOutcomeStore(pool), pool: pool}
	})
}
