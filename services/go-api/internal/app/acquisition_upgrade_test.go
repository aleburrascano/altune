package app

import (
	"altune/go-api/internal/shared/config"
	"context"
	"sync"
	"testing"

	acqService "altune/go-api/internal/acquisition/service"

	"github.com/jackc/pgx/v5/pgxpool"
)

func lazyPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), "postgres://u:p@127.0.0.1:1/altune")
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func idleScheduler(t *testing.T) *acqService.BackgroundAcquisitionScheduler {
	t.Helper()
	var wg sync.WaitGroup
	scheduler := acqService.NewBackgroundAcquisitionScheduler(nil, &wg, make(chan struct{}, 1))
	t.Cleanup(func() { scheduler.Shutdown(context.Background()) })
	return scheduler
}

func startedJobNames(a *App) []jobName {
	names := make([]jobName, 0, len(a.backgroundStarts))
	for _, job := range a.backgroundStarts {
		names = append(names, job.name)
	}
	return names
}

func TestStartAcquisitionUpgradeIfEnabled_StartsNothingUnlessEveryConditionHolds(t *testing.T) {
	cases := map[string]struct {
		enabled   bool
		pool      bool
		scheduler bool
	}{
		"flag off":     {enabled: false, pool: true, scheduler: true},
		"no database":  {enabled: true, pool: false, scheduler: true},
		"no scheduler": {enabled: true, pool: true, scheduler: false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			a := &App{cfg: &config.Config{AcquisitionUpgradeEnabled: tc.enabled, AcquisitionUpgradePerHour: 30}}
			if tc.pool {
				a.pool = lazyPool(t)
			}
			var scheduler *acqService.BackgroundAcquisitionScheduler
			if tc.scheduler {
				scheduler = idleScheduler(t)
			}

			a.startAcquisitionUpgradeIfEnabled(context.Background(), scheduler)

			if got := startedJobNames(a); len(got) != 0 {
				t.Errorf("started %v, want no upgrade job", got)
			}
		})
	}
}

func TestStartAcquisitionUpgradeIfEnabled_StartsTheUpgradeJobWhenEnabled(t *testing.T) {
	a := &App{
		cfg:  &config.Config{AcquisitionUpgradeEnabled: true, AcquisitionUpgradePerHour: 30, AcquisitionUpgradeConcurrency: 1},
		pool: lazyPool(t),
	}

	a.startAcquisitionUpgradeIfEnabled(context.Background(), idleScheduler(t))

	got := startedJobNames(a)
	if len(got) != 1 || got[0] != jobAcquisitionUpgrade {
		t.Errorf("started %v, want [%s]", got, jobAcquisitionUpgrade)
	}
}
