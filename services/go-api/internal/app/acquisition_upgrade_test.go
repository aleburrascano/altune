package app

import (
	"altune/go-api/internal/acquisition"
	"altune/go-api/internal/app/jobs"
	"altune/go-api/internal/shared/config"
	"context"
	"sync"
	"testing"

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

func idleScheduler(t *testing.T) *acquisition.BackgroundAcquisitionScheduler {
	t.Helper()
	var wg sync.WaitGroup
	scheduler := acquisition.NewBackgroundAcquisitionScheduler(nil, &wg, make(chan struct{}, 1))
	t.Cleanup(func() { scheduler.Shutdown(context.Background()) })
	return scheduler
}

func registeredJobNames(a *App) []jobs.Name {
	health := a.jobs.Health()
	names := make([]jobs.Name, 0, len(health))
	for _, h := range health {
		names = append(names, jobs.Name(h.Name))
	}
	return names
}

func TestStartAcquisitionUpgrade_StartsNothingWithoutADatabaseOrAScheduler(t *testing.T) {
	cases := map[string]struct {
		pool      bool
		scheduler bool
	}{
		"no database":  {pool: false, scheduler: true},
		"no scheduler": {pool: true, scheduler: false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			a := &App{cfg: &config.Config{AcquisitionUpgradePerHour: 30}}
			if tc.pool {
				a.pool = lazyPool(t)
			}
			var scheduler *acquisition.BackgroundAcquisitionScheduler
			if tc.scheduler {
				scheduler = idleScheduler(t)
			}

			a.startAcquisitionUpgrade(context.Background(), scheduler)

			if got := registeredJobNames(a); len(got) != 0 {
				t.Errorf("started %v, want no upgrade job", got)
			}
		})
	}
}

func TestStartAcquisitionUpgrade_StartsTheUpgradeJobWithADatabaseAndAScheduler(t *testing.T) {
	build := func(t *testing.T) *App {
		a := &App{
			cfg:  &config.Config{AcquisitionUpgradePerHour: 30, AcquisitionUpgradeConcurrency: 1},
			pool: lazyPool(t),
		}
		a.startAcquisitionUpgrade(context.Background(), idleScheduler(t))
		if got := registeredJobNames(a); len(got) != 1 || got[0] != jobs.AcquisitionUpgrade {
			t.Fatalf("registered %v, want [%s]", got, jobs.AcquisitionUpgrade)
		}
		return a
	}

	assertLeaderOnly(t, jobs.AcquisitionUpgrade, build)
}
