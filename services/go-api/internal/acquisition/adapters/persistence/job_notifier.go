package persistence

import (
	"altune/go-api/internal/acquisition/ports"
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const notifierReconnectDelay = time.Second

type PgxJobNotifier struct {
	pool *pgxpool.Pool
}

var _ ports.JobNotifier = (*PgxJobNotifier)(nil)

func NewPgxJobNotifier(pool *pgxpool.Pool) *PgxJobNotifier {
	return &PgxJobNotifier{pool: pool}
}

func (n *PgxJobNotifier) Listen(ctx context.Context, wake chan<- struct{}) {
	for ctx.Err() == nil {
		n.listenOnce(ctx, wake)
		select {
		case <-time.After(notifierReconnectDelay):
		case <-ctx.Done():
			return
		}
	}
}

func (n *PgxJobNotifier) listenOnce(ctx context.Context, wake chan<- struct{}) {
	conn, err := n.pool.Acquire(ctx)
	if err != nil {
		return
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "LISTEN "+acquisitionJobChannel); err != nil {
		return
	}
	for {
		if _, err := conn.Conn().WaitForNotification(ctx); err != nil {
			return
		}
		select {
		case wake <- struct{}{}:
		default:
		}
	}
}
