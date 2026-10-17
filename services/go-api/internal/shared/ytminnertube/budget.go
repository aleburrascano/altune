package ytminnertube

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	sharedRedis "altune/go-api/internal/shared/redis"
	goredis "github.com/redis/go-redis/v9"
)

const (
	budgetKey            = "ytminnertube:budget"
	minIntervalVarName   = "YTM_INNERTUBE_MIN_INTERVAL"
	cooldownVarName      = "YTM_INNERTUBE_COOLDOWN"
	redisURLVarName      = "REDIS_URL"
	toolRedisPoolSize    = 2
	DefaultMinInterval   = 2 * time.Second
	DefaultCooldown      = 10 * time.Minute
	unlimitedWaitMillis  = -1
	refusedWaitMillis    = -1
	redisCommandDeadline = 3 * time.Second
	minRequestTime       = 3 * time.Second
)

var (
	ErrBudgetRefused   = errors.New("ytminnertube: the shared budget cannot serve this call before its deadline")
	errBadMinInterval  = errors.New(minIntervalVarName + " must be a duration of zero or more, like 2s")
	errBadCooldown     = errors.New(cooldownVarName + " must be a duration of zero or more, like 10m")
	ErrNoRedisForTool  = errors.New(redisURLVarName + " is not set: the shared InnerTube budget lives in Redis")
	errBudgetNeedsHost = errors.New("ytminnertube: a budget needs a redis client")
)

var reserveScript = goredis.NewScript(`
local t = redis.call('TIME')
local now = t[1] * 1000 + math.floor(t[2] / 1000)
local slot = tonumber(redis.call('GET', KEYS[1]) or '0')
if slot < now then slot = now end
local wait = slot - now
local maxWait = tonumber(ARGV[2])
if maxWait >= 0 and wait > maxWait then return -1 end
local interval = tonumber(ARGV[1])
redis.call('SET', KEYS[1], string.format('%d', slot + interval), 'PX', wait + interval + 60000)
return wait
`)

var coolScript = goredis.NewScript(`
local t = redis.call('TIME')
local now = t[1] * 1000 + math.floor(t[2] / 1000)
local resumeAt = now + tonumber(ARGV[1])
local slot = tonumber(redis.call('GET', KEYS[1]) or '0')
if slot < resumeAt then
  redis.call('SET', KEYS[1], string.format('%d', resumeAt), 'PX', tonumber(ARGV[1]) + 60000)
end
return 0
`)

type Settings struct {
	MinInterval time.Duration
	Cooldown    time.Duration
}

func SettingsFromEnv(env func(string) string) (Settings, error) {
	settings := Settings{MinInterval: DefaultMinInterval, Cooldown: DefaultCooldown}
	if raw := env(minIntervalVarName); raw != "" {
		interval, err := time.ParseDuration(raw)
		if err != nil || interval < 0 {
			return Settings{}, errBadMinInterval
		}
		settings.MinInterval = interval
	}
	if raw := env(cooldownVarName); raw != "" {
		cooldown, err := time.ParseDuration(raw)
		if err != nil || cooldown < 0 {
			return Settings{}, errBadCooldown
		}
		settings.Cooldown = cooldown
	}
	return settings, nil
}

func ToolClient(ctx context.Context, env func(string) string) (*http.Client, Settings, error) {
	settings, err := SettingsFromEnv(env)
	if err != nil {
		return nil, Settings{}, err
	}
	if env(redisURLVarName) == "" {
		return nil, Settings{}, ErrNoRedisForTool
	}
	budget, err := NewBudget(sharedRedis.NewClient(ctx, env(redisURLVarName), toolRedisPoolSize), settings)
	if err != nil {
		return nil, Settings{}, err
	}
	return &http.Client{Transport: budget.Transport(nil)}, settings, nil
}

func (s Settings) Describe() string {
	return fmt.Sprintf("shared InnerTube budget: one call per %v across the app and the tools, %v cooldown after a 403", s.MinInterval, s.Cooldown)
}

type Budget struct {
	redis    *goredis.Client
	settings Settings
}

func NewBudget(client *goredis.Client, settings Settings) (*Budget, error) {
	if client == nil {
		return nil, errBudgetNeedsHost
	}
	return &Budget{redis: client, settings: settings}, nil
}

func (b *Budget) Wait(ctx context.Context) error {
	reply, err := b.reserve(ctx)
	if err != nil {
		return fmt.Errorf("ytminnertube: reserve a slot: %w", err)
	}
	if reply == refusedWaitMillis {
		return ErrBudgetRefused
	}
	timer := time.NewTimer(time.Duration(reply) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (b *Budget) reserve(ctx context.Context) (int64, error) {
	commandCtx, cancel := context.WithTimeout(ctx, redisCommandDeadline)
	defer cancel()
	return reserveScript.Run(commandCtx, b.redis, []string{budgetKey}, b.settings.MinInterval.Milliseconds(), maxWaitMillis(ctx)).Int64()
}

func maxWaitMillis(ctx context.Context) int64 {
	deadline, ok := ctx.Deadline()
	if !ok {
		return unlimitedWaitMillis
	}
	return max((time.Until(deadline) - minRequestTime).Milliseconds(), 0)
}

func (b *Budget) CoolDown(ctx context.Context) error {
	commandCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), redisCommandDeadline)
	defer cancel()
	return coolScript.Run(commandCtx, b.redis, []string{budgetKey}, b.settings.Cooldown.Milliseconds()).Err()
}

func (b *Budget) Transport(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return budgetTransport{budget: b, base: base}
}

type budgetTransport struct {
	budget *Budget
	base   http.RoundTripper
}

func (t budgetTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := t.budget.Wait(req.Context()); err != nil {
		return nil, err
	}
	resp, err := t.base.RoundTrip(req)
	if err == nil && resp != nil && resp.StatusCode == http.StatusForbidden {
		if coolErr := t.budget.CoolDown(req.Context()); coolErr != nil {
			slog.WarnContext(req.Context(), "ytminnertube: could not start the cooldown after a 403", "error", coolErr)
		}
	}
	return resp, err
}
