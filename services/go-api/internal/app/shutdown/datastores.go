package shutdown

import (
	"context"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
)

type Datastores struct {
	Pool  *pgxpool.Pool
	Redis *goredis.Client
}

func CloseDatastores(ctx context.Context, outcomes []Outcome, ds Datastores) {
	if unstopped := unfinished(outcomes); len(unstopped) > 0 {
		slog.WarnContext(ctx, "closing DB/Redis while components are still shutting down",
			"components", strings.Join(unstopped, ", "))
	}
	if leadershipRetained(outcomes) {
		slog.ErrorContext(ctx, "leaving DB pool open so process exit releases the retained leader lock")
	} else if ds.Pool != nil {
		ds.Pool.Close()
	}
	if ds.Redis != nil {
		if err := ds.Redis.Close(); err != nil {
			slog.ErrorContext(ctx, "redis client close error", "error", err)
		}
	}
}
