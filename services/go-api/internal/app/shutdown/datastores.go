package shutdown

import (
	"altune/go-api/internal/shared/logging"
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
	switch {
	case leadershipRetained(outcomes):
		slog.ErrorContext(ctx, "leaving DB pool open so process exit releases the retained leader lock")
	case ds.Pool != nil:
		ds.Pool.Close()
	}
	closeRedis(ctx, ds.Redis)
}

func closeRedis(ctx context.Context, client *goredis.Client) {
	if client == nil {
		return
	}
	if err := client.Close(); err != nil {
		slog.ErrorContext(ctx, "redis client close error", logging.KeyError, err)
	}
}
