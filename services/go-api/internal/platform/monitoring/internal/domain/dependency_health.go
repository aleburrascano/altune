package domain

import "time"

type DepStatus string

const (
	DepUp            DepStatus = "ok"
	DepNotConfigured DepStatus = "not_configured"
	DepDown          DepStatus = "down"
)

type DependencyDetail struct {
	DBLatencyMs    int64
	DBError        string
	RedisLatencyMs int64
	RedisError     string
	AuthLatencyMs  int64
	AuthError      string
	CheckedAt      time.Time
}

type DependencyHealth struct {
	DB     DepStatus
	Redis  DepStatus
	Auth   DepStatus
	Detail DependencyDetail
}

func (d DependencyHealth) Healthy() bool {
	return d.DB != DepDown && d.Redis != DepDown && d.Auth != DepDown
}
