package app

import (
	"altune/overseer/internal/buckets/backendperf"
	"altune/overseer/internal/buckets/cost"
	"altune/overseer/internal/buckets/domainquality"
	"altune/overseer/internal/buckets/heartbeat"
	"altune/overseer/internal/buckets/liveactivity"
	"altune/overseer/internal/buckets/logs"
	"altune/overseer/internal/buckets/reliability"
	"altune/overseer/internal/buckets/security"
	"altune/overseer/internal/buckets/usage"
	"altune/overseer/internal/config"
	"altune/overseer/internal/core"
	"altune/overseer/internal/goapi"
	"altune/overseer/internal/oci"
	"context"
	"log/slog"
	"strings"
	"time"
)

func Buckets(cfg *config.Config, getenv func(string) string) []core.Bucket {
	api := goAPI{base: strings.TrimSpace(getenv("OVERSEER_GOAPI_URL"))}
	return []core.Bucket{
		heartbeat.New(heartbeat.Deps{Now: time.Now}),
		liveactivity.New(liveactivity.Deps{Source: api.eventStream("liveactivity")}),
		logs.New(logs.Deps{Source: api.logStream(), MinLevel: getenv("OVERSEER_LOGS_MIN_LEVEL")}),
		usage.New(usage.Deps{Source: api.eventStream("usage")}),
		backendPerf(api),
		domainQuality(api),
		costBucket(cfg, api),
		reliabilityBucket(api, getenv),
		securityBucket(api, getenv),
	}
}

type goAPI struct {
	base string
}

type eventStream interface {
	Run(ctx context.Context) error
	Events() <-chan goapi.Event
	Status() goapi.Status
}

func (a goAPI) eventStream(bucket string) eventStream {
	if a.base == "" {
		return nil
	}
	c, err := goapi.NewConsumer(a.base, goapi.SharedTokenSource())
	if err != nil {
		slog.Warn(bucket+": invalid OVERSEER_GOAPI_URL, degrading to source-down", "error", err)
		return nil
	}
	return c
}

type logStream interface {
	Run(ctx context.Context) error
	Records() <-chan goapi.LogRecord
	Status() goapi.Status
}

func (a goAPI) logStream() logStream {
	if a.base == "" {
		return nil
	}
	c, err := goapi.NewLogsConsumer(a.base, goapi.SharedTokenSource())
	if err != nil {
		slog.Warn("logs: invalid OVERSEER_GOAPI_URL, degrading to source-down", "error", err)
		return nil
	}
	return c
}

func (a goAPI) client(warning string) *goapi.Client {
	if a.base == "" {
		return nil
	}
	c, err := goapi.New(a.base, goapi.SharedTokenSource())
	if err != nil {
		slog.Warn(warning, "error", err)
		return nil
	}
	return c
}

func backendPerf(api goAPI) *backendperf.Bucket {
	var deps backendperf.Deps
	if c := api.client("backendperf: invalid OVERSEER_GOAPI_URL, degrading to source-down"); c != nil {
		deps.Reader = c
	}
	return backendperf.New(deps)
}

func domainQuality(api goAPI) *domainquality.Bucket {
	var deps domainquality.Deps
	if c := api.client("domainquality: invalid OVERSEER_GOAPI_URL, degrading to source-down"); c != nil {
		deps.Reader = c
	}
	return domainquality.New(deps)
}

func securityBucket(api goAPI, getenv func(string) string) *security.Bucket {
	deps := security.Deps{Interval: intervalEnv(getenv, "security", "OVERSEER_SECURITY_INTERVAL")}
	if api.base == "" {
		return security.New(deps)
	}
	c, err := security.NewFencedClient(api.base, securityAllowlist(api.base, getenv))
	if err != nil {
		slog.Warn("security: cannot build fenced client, degrading to source-down", "error", err)
		return security.New(deps)
	}
	deps.Prober = c
	return security.New(deps)
}

func securityAllowlist(base string, getenv func(string) string) []string {
	raw := strings.TrimSpace(getenv("OVERSEER_SECURITY_ALLOWLIST"))
	if raw == "" {
		return []string{base}
	}
	return strings.Split(raw, ",")
}

func costBucket(cfg *config.Config, api goAPI) *cost.Bucket {
	deps := cost.Deps{SpendInterval: cfg.CostSpendInterval}
	if cfg.OCIEnabled {
		deps.ConnectSpend = func() (cost.SpendReader, error) { return oci.NewFromInstancePrincipal() }
	}
	if c := api.client("cost: invalid OVERSEER_GOAPI_URL, degrading provider-usage to source-down"); c != nil {
		deps.Usage = c
	}
	return cost.New(deps)
}

func reliabilityBucket(api goAPI, getenv func(string) string) *reliability.Bucket {
	var deps reliability.Deps
	if c := api.client("reliability: invalid OVERSEER_GOAPI_URL, degrading to source-down"); c != nil {
		deps.Reader = c
		deps.Checker = c
	}
	deps.Interval = intervalEnv(getenv, "reliability", "OVERSEER_RELIABILITY_POLL_INTERVAL")
	return reliability.New(deps)
}

func intervalEnv(getenv func(string) string, bucket, key string) time.Duration {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return 0
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		slog.Warn(bucket+": invalid "+key+", using default", "value", raw)
		return 0
	}
	return d
}
