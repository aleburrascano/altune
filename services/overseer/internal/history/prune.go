package history

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

const (
	pruneInterval = 10 * time.Minute
	pruneBatch    = 2000
	minuteOfAt    = "(at - ((at % 60000) + 60000) % 60000)"
)

const dropExpiredPointsSQL = `DELETE FROM points WHERE rowid IN (SELECT rowid FROM points WHERE at < ? LIMIT ?)`

const dropExpiredRollupsSQL = `DELETE FROM rollups WHERE rowid IN (SELECT rowid FROM rollups WHERE minute < ? LIMIT ?)`

const foldBoundSQL = `SELECT at FROM points WHERE at < ? ORDER BY at LIMIT 1 OFFSET ?`

const foldSQL = `
INSERT INTO rollups (bucket, series, minute, lowest, highest, total, samples)
SELECT bucket, series, ` + minuteOfAt + `, MIN(value), MAX(value), SUM(value), COUNT(*)
FROM points INDEXED BY points_at WHERE at < ?
GROUP BY bucket, series, ` + minuteOfAt + `
ON CONFLICT (bucket, series, minute) DO UPDATE SET
	lowest = MIN(lowest, excluded.lowest),
	highest = MAX(highest, excluded.highest),
	total = total + excluded.total,
	samples = samples + excluded.samples`

const dropFoldedPointsSQL = `DELETE FROM points WHERE at < ?`

const pointsOverCapSQL = `SELECT bucket, series FROM points GROUP BY bucket, series HAVING COUNT(*) > ?`

const rollupsOverCapSQL = `SELECT bucket, series FROM rollups GROUP BY bucket, series HAVING COUNT(*) > ?`

const trimRollupsSQL = `
DELETE FROM rollups WHERE rowid IN (
	SELECT rowid FROM rollups
	WHERE bucket = ? AND series = ?
	ORDER BY minute DESC
	LIMIT -1 OFFSET ?
)`

func (d *disk) RunPruner(ctx context.Context) {
	d.logPrune(ctx)
	ticker := time.NewTicker(pruneInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.logPrune(ctx)
		}
	}
}

func (d *disk) logPrune(ctx context.Context) {
	if err := d.prune(ctx); err != nil && ctx.Err() == nil {
		slog.WarnContext(ctx, "history.prune_failed", "error", err)
	}
}

func (d *disk) prune(ctx context.Context) error {
	now := d.now()
	expired := now.Add(-d.retention).UnixMilli()
	if err := d.deleteInBatches(ctx, dropExpiredPointsSQL, expired); err != nil {
		return fmt.Errorf("drop expired points: %w", err)
	}
	if err := d.foldRawBefore(ctx, now.Add(-RawRetention).UnixMilli()); err != nil {
		return fmt.Errorf("fold raw points into rollups: %w", err)
	}
	if err := d.deleteInBatches(ctx, dropExpiredRollupsSQL, expired); err != nil {
		return fmt.Errorf("drop expired rollups: %w", err)
	}
	if err := d.trimOverCap(ctx, pointsOverCapSQL, trimPointsSQL); err != nil {
		return fmt.Errorf("trim points: %w", err)
	}
	if err := d.trimOverCap(ctx, rollupsOverCapSQL, trimRollupsSQL); err != nil {
		return fmt.Errorf("trim rollups: %w", err)
	}
	return nil
}

func (d *disk) deleteInBatches(ctx context.Context, statement string, cutoffMillis int64) error {
	for {
		deleted, err := d.deleteBatch(ctx, statement, cutoffMillis)
		if err != nil || deleted < pruneBatch {
			return err
		}
	}
}

func (d *disk) deleteBatch(ctx context.Context, statement string, cutoffMillis int64) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	result, err := d.db.ExecContext(ctx, statement, cutoffMillis, pruneBatch)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (d *disk) foldRawBefore(ctx context.Context, cutoffMillis int64) error {
	for {
		folded, err := d.foldBatch(ctx, cutoffMillis)
		if err != nil || folded < pruneBatch {
			return err
		}
	}
}

func (d *disk) foldBatch(ctx context.Context, cutoffMillis int64) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	folded, err := foldOldest(ctx, tx, cutoffMillis)
	if err != nil {
		return 0, errors.Join(err, tx.Rollback())
	}
	return folded, tx.Commit()
}

func foldOldest(ctx context.Context, tx *sql.Tx, cutoffMillis int64) (int64, error) {
	bound, err := foldBound(ctx, tx, cutoffMillis)
	if err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, foldSQL, bound); err != nil {
		return 0, err
	}
	result, err := tx.ExecContext(ctx, dropFoldedPointsSQL, bound)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func foldBound(ctx context.Context, tx *sql.Tx, cutoffMillis int64) (int64, error) {
	var lastInBatch int64
	err := tx.QueryRowContext(ctx, foldBoundSQL, cutoffMillis, pruneBatch-1).Scan(&lastInBatch)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return cutoffMillis, nil
	case err != nil:
		return 0, err
	}
	return lastInBatch + 1, nil
}

type seriesKey struct {
	bucket string
	series string
}

func (d *disk) trimOverCap(ctx context.Context, overCapSQL, trimSQL string) error {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	overCap, err := d.seriesOverCap(ctx, overCapSQL)
	if err != nil {
		return err
	}
	for _, key := range overCap {
		if _, err := d.db.ExecContext(ctx, trimSQL, key.bucket, key.series, d.rowCap); err != nil {
			return fmt.Errorf("trim %s/%s: %w", key.bucket, key.series, err)
		}
	}
	return nil
}

func (d *disk) seriesOverCap(ctx context.Context, overCapSQL string) ([]seriesKey, error) {
	rows, err := d.db.QueryContext(ctx, overCapSQL, d.rowCap)
	if err != nil {
		return nil, fmt.Errorf("find series over cap: %w", err)
	}
	return scanAll(rows, func(rows *sql.Rows) (seriesKey, error) {
		var key seriesKey
		err := rows.Scan(&key.bucket, &key.series)
		return key, err
	})
}
