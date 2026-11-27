package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func InTx(ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx) error) error {
	if err := pgx.BeginTxFunc(ctx, pool, pgx.TxOptions{AccessMode: pgx.ReadWrite}, fn); err != nil {
		return fmt.Errorf("begin tx func: %w", err)
	}
	return nil
}
