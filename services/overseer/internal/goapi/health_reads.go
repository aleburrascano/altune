package goapi

import (
	"context"
	"net/http"
	"time"
)

// observeHealthPath is go-api's operator dependency-health endpoint. It is mounted
// under the observe-guarded "/observe" group (internal/app/observe_wiring.go), so
// the request must carry the read-only bearer the client already attaches.
const observeHealthPath = "/observe/health"

const statusDown = "down"

// OperatorHealth is go-api's operator dependency-health snapshot from
// GET /observe/health: per-dependency status pills (DB/Redis/Auth), a detail block
// with probe latencies and errors, and process gauges. It mirrors go-api's
// response shape; unknown fields a newer go-api adds are ignored, so the mirror
// tolerates a version skew rather than failing the read.
type OperatorHealth struct {
	DB         string       `json:"db"`
	Redis      string       `json:"redis"`
	Auth       string       `json:"auth"`
	Detail     HealthDetail `json:"detail"`
	Goroutines int          `json:"goroutines"`
	HeapMB     uint64       `json:"heap_mb"`
}

// HealthDetail carries the per-dependency probe latencies and error strings from
// the operator health snapshot. Error fields are empty when the dependency is
// healthy (go-api omits them), so a non-empty value names what is wrong.
type HealthDetail struct {
	DBLatencyMs    int64     `json:"db_latency_ms"`
	DBError        string    `json:"db_error,omitempty"`
	RedisLatencyMs int64     `json:"redis_latency_ms"`
	RedisError     string    `json:"redis_error,omitempty"`
	AuthLatencyMs  int64     `json:"auth_latency_ms"`
	AuthError      string    `json:"auth_error,omitempty"`
	CheckedAt      time.Time `json:"checked_at"`
}

// Healthy reports whether every observed dependency is not down, matching
// go-api's own verdict (health_handler.go). It treats an unknown status as
// healthy-until-proven-down, deferring to go-api rather than second-guessing it.
func (h OperatorHealth) Healthy() bool {
	return h.DB != statusDown && h.Redis != statusDown && h.Auth != statusDown
}

// AdminHealth fetches GET /observe/health, go-api's operator dependency-health
// snapshot, decoded into OperatorHealth. It reuses the read primitive, so the
// read-only bearer, the host pin, the bounded body and the timeout all apply: an
// unreachable go-api yields a SourceDownError, a rejected token or a principal the admin gate refuses
// principal yields an APIError, and a runaway body cannot exhaust memory. It is
// a read; nothing here writes, commands or mutates go-api.
func (c *Client) AdminHealth(ctx context.Context) (OperatorHealth, error) {
	var out OperatorHealth
	if err := c.get(ctx, observeHealthPath, &out, http.StatusServiceUnavailable); err != nil {
		return OperatorHealth{}, err
	}
	return out, nil
}
