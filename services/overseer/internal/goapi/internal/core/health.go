package core

import "time"

type Health struct {
	Status string `json:"status"`
}

const healthStatusDegraded = "degraded"

func (h Health) OK() bool { return h.Status == "ok" }

func (h Health) Degraded() bool { return h.Status == healthStatusDegraded }

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
