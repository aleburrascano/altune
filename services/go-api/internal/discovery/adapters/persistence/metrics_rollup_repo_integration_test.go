//go:build integration

package persistence

import (
	"context"
	"testing"
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared"

	"github.com/google/uuid"
)

// The rollup is tested on a synthetic day far in the past so the aggregate
// window can't be polluted by real dev-DB events (which all carry recent
// occurred_at) or by the other event tests in this package.
var rollupTestDay = time.Date(2001, 2, 3, 0, 0, 0, 0, time.UTC)

func TestPgxMetricsRollup_RollupDay_ComputesAndIsIdempotent(t *testing.T) {
	pool := testPool(t)
	store := NewPgxEventStore(pool)
	rollup := NewPgxMetricsRollup(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM discovery_events WHERE user_id = $1`, userId.UUID())
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM discovery_metrics WHERE as_of = $1`, rollupTestDay)
	})

	append_ := func(evType domain.EventType, searchID string, payload map[string]any, offset time.Duration) {
		t.Helper()
		if err := store.Append(ctx, domain.InteractionEvent{
			UserId: userId, Type: evType, SearchId: searchID,
			Payload: payload, OccurredAt: rollupTestDay.Add(offset),
		}); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	// 4 searches in the day: 1 zero-result, 1 poisoned zero_result (string) and
	// 1 poisoned tail_noise_top5 (string) that must be SKIPPED not fatal, plus
	// tail values 2 and 4 (avg 3.0). Distinct clicked search_ids: 2 (one search
	// clicked twice must count once).
	s1, s2 := uuid.New().String(), uuid.New().String()
	append_(domain.EventTypeSearchPerformed, s1,
		map[string]any{"zero_result": true, "tail_noise_top5": 2}, 1*time.Hour)
	append_(domain.EventTypeSearchPerformed, s2,
		map[string]any{"zero_result": false, "tail_noise_top5": 4}, 2*time.Hour)
	append_(domain.EventTypeSearchPerformed, uuid.New().String(),
		map[string]any{"zero_result": "abc", "tail_noise_top5": "xyz"}, 3*time.Hour)
	append_(domain.EventTypeSearchPerformed, uuid.New().String(), nil, 4*time.Hour)

	append_(domain.EventTypeResultClicked, s1, nil, 1*time.Hour+time.Minute)
	append_(domain.EventTypeResultClicked, s1, nil, 1*time.Hour+2*time.Minute) // same search: counts once
	append_(domain.EventTypeResultClicked, s2, nil, 2*time.Hour+time.Minute)

	// An event just OUTSIDE the day must not leak into the window.
	append_(domain.EventTypeSearchPerformed, uuid.New().String(),
		map[string]any{"zero_result": true}, 24*time.Hour)

	if err := rollup.RollupDay(ctx, rollupTestDay.Add(13*time.Hour)); err != nil {
		t.Fatalf("RollupDay: %v (poisoned payloads must not fail the rollup)", err)
	}

	readMetrics := func(t *testing.T) map[string]float64 {
		t.Helper()
		rows, err := pool.Query(ctx,
			`SELECT metric, value FROM discovery_metrics WHERE as_of = $1`, rollupTestDay)
		if err != nil {
			t.Fatalf("read metrics: %v", err)
		}
		defer rows.Close()
		got := map[string]float64{}
		for rows.Next() {
			var m string
			var v float64
			if err := rows.Scan(&m, &v); err != nil {
				t.Fatalf("scan metric: %v", err)
			}
			got[m] = v
		}
		return got
	}

	got := readMetrics(t)
	want := map[string]float64{
		"searches":            4,
		"zero_result_rate":    0.25, // 1 boolean-true of 4 (poisoned string skipped)
		"ctr":                 0.5,  // 2 distinct clicked searches of 4
		"tail_noise_top5_avg": 3,    // avg(2, 4); poisoned string skipped
	}
	for metric, wantV := range want {
		gotV, ok := got[metric]
		if !ok {
			t.Errorf("metric %q missing from rollup", metric)
			continue
		}
		if diff := gotV - wantV; diff > 1e-9 || diff < -1e-9 {
			t.Errorf("%s = %v, want %v", metric, gotV, wantV)
		}
	}

	// Idempotent re-run: same day again → still exactly 4 rows, same values
	// (ON CONFLICT upserts, never duplicates).
	if err := rollup.RollupDay(ctx, rollupTestDay); err != nil {
		t.Fatalf("RollupDay re-run: %v", err)
	}
	rerun := readMetrics(t)
	if len(rerun) != 4 {
		t.Errorf("metric rows after re-run = %d, want 4 (upsert, not append)", len(rerun))
	}
	for metric, wantV := range want {
		if diff := rerun[metric] - wantV; diff > 1e-9 || diff < -1e-9 {
			t.Errorf("after re-run %s = %v, want %v", metric, rerun[metric], wantV)
		}
	}
}

func TestPgxMetricsRollup_MetricsHistory_NewestFirstAndLimited(t *testing.T) {
	pool := testPool(t)
	rollup := NewPgxMetricsRollup(pool)
	ctx := context.Background()

	metric := "qa_test_metric_" + uuid.New().String()[:8]
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM discovery_metrics WHERE metric = $1`, metric)
	})

	days := []struct {
		asOf  time.Time
		value float64
	}{
		{time.Date(2001, 5, 1, 0, 0, 0, 0, time.UTC), 0.1},
		{time.Date(2001, 5, 2, 0, 0, 0, 0, time.UTC), 0.2},
		{time.Date(2001, 5, 3, 0, 0, 0, 0, time.UTC), 0.3},
	}
	for _, d := range days {
		if _, err := pool.Exec(ctx,
			`INSERT INTO discovery_metrics (as_of, metric, value) VALUES ($1, $2, $3)`,
			d.asOf, metric, d.value); err != nil {
			t.Fatalf("seed metric row: %v", err)
		}
	}

	points, err := rollup.MetricsHistory(ctx, metric, 2)
	if err != nil {
		t.Fatalf("MetricsHistory: %v", err)
	}
	if len(points) != 2 {
		t.Fatalf("len(points) = %d, want 2 (days limit)", len(points))
	}
	if points[0].Value != 0.3 || points[1].Value != 0.2 {
		t.Errorf("points = [%v, %v], want [0.3, 0.2] (newest first)", points[0].Value, points[1].Value)
	}
	if !points[0].AsOf.After(points[1].AsOf) {
		t.Errorf("as_of order: %v then %v, want descending", points[0].AsOf, points[1].AsOf)
	}

	// An unknown metric is an empty history, not an error.
	empty, err := rollup.MetricsHistory(ctx, "qa_never_written_"+uuid.New().String()[:8], 7)
	if err != nil {
		t.Fatalf("MetricsHistory(unknown): %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("unknown metric returned %d points, want 0", len(empty))
	}
}
