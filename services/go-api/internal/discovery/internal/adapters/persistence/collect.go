package persistence

import (
	"errors"

	"github.com/jackc/pgx/v5"
)

var errSkipRow = errors.New("skip row")

func collectRows[T any](rows pgx.Rows, scan func(pgx.Rows) (T, error)) ([]T, error) {
	out := []T{}
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			if errors.Is(err, errSkipRow) {
				continue
			}
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
