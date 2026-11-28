package persistence

import "github.com/jackc/pgx/v5/pgxpool"

type Database = *pgxpool.Pool
