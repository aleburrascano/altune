package history

import (
	"altune/overseer/internal/core"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite"
)

const (
	pruneInterval = 10 * time.Minute
	opTimeout     = 5 * time.Second
)

const schema = `
CREATE TABLE IF NOT EXISTS points (
	bucket TEXT NOT NULL,
	series TEXT NOT NULL,
	at INTEGER NOT NULL,
	value REAL NOT NULL
);
CREATE INDEX IF NOT EXISTS points_bucket_series_at ON points (bucket, series, at);
`

const trimSeriesSQL = `
DELETE FROM points WHERE rowid IN (
	SELECT rowid FROM points
	WHERE bucket = ? AND series = ?
	ORDER BY at DESC, rowid DESC
	LIMIT -1 OFFSET ?
)`

var errNonFinite = errors.New("history: point value is not a finite number")

type Option func(*disk)

func WithCap(rows int) Option {
	return func(d *disk) {
		if rows > 0 {
			d.rowCap = rows
		}
	}
}

func WithRetention(age time.Duration) Option {
	return func(d *disk) {
		if age > 0 {
			d.retention = age
		}
	}
}

func WithClock(now func() time.Time) Option {
	return func(d *disk) { d.now = now }
}

type disk struct {
	db           *sql.DB
	rowCap       int
	retention    time.Duration
	now          func() time.Time
	writeFailing atomic.Bool
}

func openDisk(path string, opts ...Option) (*disk, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	db.SetMaxOpenConns(1)
	d := &disk{db: db, rowCap: DefaultCap, retention: DefaultRetention, now: time.Now}
	for _, opt := range opts {
		opt(d)
	}
	if err := d.prepare(); err != nil {
		return nil, errors.Join(err, db.Close())
	}
	return d, nil
}

func (d *disk) prepare() error {
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	var verdict string
	if err := d.db.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&verdict); err != nil {
		return fmt.Errorf("integrity check: %w", err)
	}
	if verdict != "ok" {
		return fmt.Errorf("integrity check: %s", verdict)
	}
	if _, err := d.db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("create schema: %w", err)
	}
	return nil
}

func (d *disk) Record(bucket, series string, p core.Point) {
	d.noteWrite(bucket, series, d.insert(bucket, series, p))
}

func (d *disk) insert(bucket, series string, p core.Point) error {
	if math.IsNaN(p.Value) || math.IsInf(p.Value, 0) {
		return errNonFinite
	}
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO points (bucket, series, at, value) VALUES (?, ?, ?, ?)",
		bucket, series, p.At.UnixMilli(), p.Value); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	if _, err := tx.ExecContext(ctx, trimSeriesSQL, bucket, series, d.rowCap); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	return tx.Commit()
}

func (d *disk) noteWrite(bucket, series string, err error) {
	if err == nil {
		if d.writeFailing.CompareAndSwap(true, false) {
			slog.Info("history.write_recovered", "bucket", bucket, "series", series)
		}
		return
	}
	if d.writeFailing.CompareAndSwap(false, true) {
		slog.Warn("history.write_failed", "bucket", bucket, "series", series, "error", err)
	}
}

func (d *disk) Query(bucket, series string, from, to time.Time) ([]core.Point, error) {
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	rows, err := d.db.QueryContext(ctx,
		"SELECT at, value FROM points WHERE bucket = ? AND series = ? AND at >= ? AND at <= ? ORDER BY at, rowid",
		bucket, series, from.UnixMilli(), to.UnixMilli())
	if err != nil {
		return nil, err
	}
	return scanAll(rows, func(rows *sql.Rows) (core.Point, error) {
		var atMillis int64
		var value float64
		err := rows.Scan(&atMillis, &value)
		return core.Point{At: time.UnixMilli(atMillis).UTC(), Value: value}, err
	})
}

func (d *disk) Names(bucket string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	rows, err := d.db.QueryContext(ctx, "SELECT DISTINCT series FROM points WHERE bucket = ? ORDER BY series", bucket)
	if err != nil {
		return nil, err
	}
	return scanAll(rows, func(rows *sql.Rows) (string, error) {
		var name string
		err := rows.Scan(&name)
		return name, err
	})
}

func scanAll[T any](rows *sql.Rows, scanOne func(*sql.Rows) (T, error)) (scanned []T, err error) {
	defer func() { err = errors.Join(err, rows.Close()) }()
	for rows.Next() {
		value, err := scanOne(rows)
		if err != nil {
			return nil, err
		}
		scanned = append(scanned, value)
	}
	return scanned, rows.Err()
}

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
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	cutoff := d.now().Add(-d.retention).UnixMilli()
	if _, err := d.db.ExecContext(ctx, "DELETE FROM points WHERE at < ?", cutoff); err != nil {
		return fmt.Errorf("drop expired points: %w", err)
	}
	overCap, err := d.seriesOverCap(ctx)
	if err != nil {
		return err
	}
	for _, key := range overCap {
		if _, err := d.db.ExecContext(ctx, trimSeriesSQL, key.bucket, key.series, d.rowCap); err != nil {
			return fmt.Errorf("trim %s/%s: %w", key.bucket, key.series, err)
		}
	}
	return nil
}

type seriesKey struct {
	bucket string
	series string
}

func (d *disk) seriesOverCap(ctx context.Context) ([]seriesKey, error) {
	rows, err := d.db.QueryContext(ctx,
		"SELECT bucket, series FROM points GROUP BY bucket, series HAVING COUNT(*) > ?", d.rowCap)
	if err != nil {
		return nil, fmt.Errorf("find series over cap: %w", err)
	}
	return scanAll(rows, func(rows *sql.Rows) (seriesKey, error) {
		var key seriesKey
		err := rows.Scan(&key.bucket, &key.series)
		return key, err
	})
}

func (d *disk) Close() error {
	return d.db.Close()
}
