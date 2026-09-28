package goapi

import (
	"context"
	"net/http"
	"time"
)

const observeHealthPath = "/observe/health"

const statusDown = "down"

type OperatorHealth struct {
	DB         string       `json:"db"`
	Redis      string       `json:"redis"`
	Auth       string       `json:"auth"`
	Detail     HealthDetail `json:"detail"`
	Goroutines int          `json:"goroutines"`
	HeapMB     uint64       `json:"heap_mb"`
}

type HealthDetail struct {
	DBLatencyMs    int64     `json:"db_latency_ms"`
	DBError        string    `json:"db_error,omitempty"`
	RedisLatencyMs int64     `json:"redis_latency_ms"`
	RedisError     string    `json:"redis_error,omitempty"`
	AuthLatencyMs  int64     `json:"auth_latency_ms"`
	AuthError      string    `json:"auth_error,omitempty"`
	CheckedAt      time.Time `json:"checked_at"`
}

func (h OperatorHealth) Healthy() bool {
	return h.DB != statusDown && h.Redis != statusDown && h.Auth != statusDown
}

func (c *Client) AdminHealth(ctx context.Context) (OperatorHealth, error) {
	var out OperatorHealth
	if err := c.get(ctx, observeHealthPath, &out, http.StatusServiceUnavailable); err != nil {
		return OperatorHealth{}, err
	}
	return out, nil
}
