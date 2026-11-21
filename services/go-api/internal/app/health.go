package app

import (
	"altune/go-api/internal/app/health"
	"altune/go-api/internal/observe"
	"context"
)

var buildCommit = "unknown"

func BuildCommit() string { return buildCommit }

type authHealthChecker interface {
	CheckHealth(ctx context.Context) error
}

func (a *App) healthProbes() health.Probes {
	probes := health.Probes{DB: a.dbPing}
	if a.redisClient != nil {
		probes.Redis = func(ctx context.Context) error { return a.redisClient.Ping(ctx).Err() }
	}
	if a.authVerifier != nil {
		probes.Auth = a.authVerifier.CheckHealth
	}
	return probes
}

func (a *App) dependencyHealth(ctx context.Context) observe.DependencyHealth {
	return health.Check(ctx, a.healthProbes(), health.DefaultProbeTimeout)
}
