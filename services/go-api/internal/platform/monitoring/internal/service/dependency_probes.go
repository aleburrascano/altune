package service

import (
	"altune/go-api/internal/platform/monitoring/internal/domain"
	"altune/go-api/internal/shared/clock"
	"context"
	"time"
)

const DefaultProbeTimeout = 2 * time.Second

type Probes struct {
	DB    func(context.Context) error
	Redis func(context.Context) error
	Auth  func(context.Context) error
}

type probeResult struct {
	status    domain.DepStatus
	err       string
	latencyMs int64
}

func CheckDependencies(ctx context.Context, now clock.Func, probes Probes, timeout time.Duration) domain.DependencyHealth {
	if now == nil {
		panic("health.Check: a nil clock")
	}
	db := probe(ctx, now, probes.DB, timeout)
	redis := probe(ctx, now, probes.Redis, timeout)
	auth := probe(ctx, now, probes.Auth, timeout)
	return domain.DependencyHealth{DB: db.status, Redis: redis.status, Auth: auth.status, Detail: domain.DependencyDetail{
		DBLatencyMs: db.latencyMs, DBError: db.err, RedisLatencyMs: redis.latencyMs, RedisError: redis.err,
		AuthLatencyMs: auth.latencyMs, AuthError: auth.err, CheckedAt: now.UTC(),
	}}
}

func probe(ctx context.Context, now clock.Func, check func(context.Context) error, timeout time.Duration) probeResult {
	if check == nil {
		return probeResult{status: domain.DepNotConfigured}
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	start := now()
	err := check(ctx)
	ms := now().Sub(start).Milliseconds()
	if err != nil {
		return probeResult{status: domain.DepDown, err: err.Error(), latencyMs: ms}
	}
	return probeResult{status: domain.DepUp, latencyMs: ms}
}
