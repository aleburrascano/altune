package persistence_test

import (
	"altune/go-api/internal/download/internal/adapters/persistence"
	"altune/go-api/internal/download/internal/contracttest"
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/shared/dbtest"
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"pgregory.net/rapid"
)

func TestPostgresUpgradeStore_RandomRowsAreSelectedExactlyWhenTheGoSpecificationSaysSo(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := dbtest.Pool(t)
	dbtest.ExclusiveAcquisitionQueue(t, pool)
	store := persistence.NewPostgresUpgradeStore(pool)

	rapid.Check(t, func(rt *rapid.T) {
		now := time.Now()
		tried := rapid.SampledFrom([]time.Duration{0, 10 * time.Minute, 5 * time.Hour}).Draw(rt, "triedAgo")
		row := domain.UpgradeRow{
			Ready:                   rapid.Bool().Draw(rt, "ready"),
			HasAudio:                rapid.Bool().Draw(rt, "hasAudio"),
			Grade:                   rapid.SampledFrom([]domain.Grade{"", domain.F, domain.D, domain.C, domain.B, domain.A, domain.AUser, domain.BestAvailable}).Draw(rt, "grade"),
			SourceFormatID:          rapid.SampledFrom([]string{"", "18", "140"}).Draw(rt, "format"),
			SourceKbps:              rapid.SampledFrom([]int{0, 64, 112, 130}).Draw(rt, "kbps"),
			InPlaceGeneration:       rapid.IntRange(0, domain.PipelineGeneration).Draw(rt, "inPlaceGeneration"),
			BestAvailableGeneration: rapid.IntRange(0, domain.PipelineGeneration).Draw(rt, "bestAvailableGeneration"),
			JobQueued:               rapid.Bool().Draw(rt, "jobQueued"),
		}
		if rapid.Bool().Draw(rt, "hasSourceURL") {
			row.SourceURL = "https://www.youtube.com/watch?v=abcdefghijk"
		}
		if tried > 0 {
			at := now.Add(-tried)
			row.UpgradeTriedAt = &at
		}
		limits := domain.UpgradeRules{
			Generation: domain.PipelineGeneration, RetryAfter: time.Hour, MaxQueued: 1_000_000, Now: now,
			CheckInPlace:        rapid.Bool().Draw(rt, "checkInPlace"),
			LowBitrateFloorKbps: rapid.SampledFrom([]int{0, 112}).Draw(rt, "floor"),
		}

		id := insertRow(t, pool, row)
		selection := ports.UpgradeSelection{
			Generation: limits.Generation, RetryAfter: limits.RetryAfter, MaxQueued: limits.MaxQueued,
			CheckInPlace: limits.CheckInPlace, LowBitrateFloorKbps: limits.LowBitrateFloorKbps,
			Upgradable: contracttest.Rules{}.Upgradable(),
		}
		inSQL := slices.Contains(candidatesAmong(t, store, selection, id), id)

		if want := domain.UpgradeEligible(row, limits); inSQL != want {
			rt.Fatalf("row %+v with %+v: SQL selects = %v, Go specification = %v", row, limits, inSQL, want)
		}
	})
}

func TestPostgresUpgradeStore_ASelectionWithoutUpgradableStatusesIsRefusedNotEmpty(t *testing.T) {
	dbtest.RequireIntegration(t)
	store := persistence.NewPostgresUpgradeStore(dbtest.Pool(t))

	_, err := store.Candidates(context.Background(), ports.UpgradeSelection{Limit: 1, Generation: domain.PipelineGeneration})

	if !errors.Is(err, persistence.ErrNoUpgradableStatuses) {
		t.Errorf("Candidates = %v, want ErrNoUpgradableStatuses", err)
	}
}
