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
	opTimeout    = 5 * time.Second
	checkTimeout = time.Minute
)

const schema = `
CREATE TABLE IF NOT EXISTS points (
	bucket TEXT NOT NULL,
	series TEXT NOT NULL,
	at INTEGER NOT NULL,
	value REAL NOT NULL
);
CREATE INDEX IF NOT EXISTS points_bucket_series_at ON points (bucket, series, at);
CREATE INDEX IF NOT EXISTS points_at ON points (at);
CREATE TABLE IF NOT EXISTS rollups (
	bucket TEXT NOT NULL,
	series TEXT NOT NULL,
	minute INTEGER NOT NULL,
	lowest REAL NOT NULL,
	highest REAL NOT NULL,
	total REAL NOT NULL,
	samples INTEGER NOT NULL CHECK (samples > 0)
);
CREATE UNIQUE INDEX IF NOT EXISTS rollups_bucket_series_minute ON rollups (bucket, series, minute);
CREATE INDEX IF NOT EXISTS rollups_minute ON rollups (minute);
`

const querySQL = `
SELECT minute AS at, -1 AS seq, total / samples AS value FROM rollups
WHERE bucket = ?1 AND series = ?2 AND minute >= ?3 AND minute <= ?4
UNION ALL
SELECT at, rowid, value FROM points
WHERE bucket = ?1 AND series = ?2 AND at >= ?3 AND at <= ?4
ORDER BY at, seq`

const tailSQL = `
SELECT minute AS at, -1 AS seq, total / samples AS value FROM rollups
WHERE bucket = ?1 AND series = ?2 AND minute >= ?3 AND minute <= ?4
UNION ALL
SELECT at, rowid, value FROM points
WHERE bucket = ?1 AND series = ?2 AND at >= ?3 AND at <= ?4
ORDER BY at DESC, seq DESC
LIMIT ?5`

const minutesSQL = `
SELECT minute, MIN(lowest), MAX(highest), SUM(total) / SUM(samples) FROM (
	SELECT minute, lowest, highest, total, samples FROM rollups
	WHERE bucket = ?1 AND series = ?2 AND minute >= ?3 AND minute <= ?4
	UNION ALL
	SELECT ` + minuteOfAt + `, MIN(value), MAX(value), SUM(value), COUNT(*) FROM points
	WHERE bucket = ?1 AND series = ?2 AND at >= ?3 AND at <= ?4
	GROUP BY ` + minuteOfAt + `
)
GROUP BY minute
ORDER BY minute`

const namesSQL = `
SELECT series FROM points WHERE bucket = ?1
UNION
SELECT series FROM rollups WHERE bucket = ?1
ORDER BY series`

const trimPointsSQL = `
DELETE FROM points WHERE rowid IN (
	SELECT rowid FROM points
	WHERE bucket = ? AND series = ?
	ORDER BY at DESC, rowid DESC
	LIMIT -1 OFFSET ?
)`

const trimPointsBatchSQL = `
DELETE FROM points WHERE rowid IN (
	SELECT rowid FROM points
	WHERE bucket = ? AND series = ?
	ORDER BY at DESC, rowid DESC
	LIMIT ? OFFSET ?
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
	checkTimeout time.Duration
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
	d := &disk{db: db, checkTimeout: checkTimeout, rowCap: DefaultCap, retention: DefaultRetention, now: time.Now}
	for _, opt := range opts {
		opt(d)
	}
	if err := d.prepare(); err != nil {
		return nil, errors.Join(err, db.Close())
	}
	return d, nil
}

func (d *disk) prepare() error {
	if err := d.checkIntegrity(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	if _, err := d.db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("create schema: %w", err)
	}
	return nil
}

func (d *disk) checkIntegrity() error {
	ctx, cancel := context.WithTimeout(context.Background(), d.checkTimeout)
	defer cancel()
	var verdict string
	err := d.db.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&verdict)
	switch {
	case err != nil && errors.Is(ctx.Err(), context.DeadlineExceeded):
		slog.Warn("history.integrity_check_timed_out", "timeout", d.checkTimeout)
		return nil
	case err != nil:
		return fmt.Errorf("integrity check: %w", err)
	case verdict != "ok":
		return fmt.Errorf("integrity check: %s", verdict)
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
	if _, err := tx.ExecContext(ctx, trimPointsSQL, bucket, series, d.rowCap); err != nil {
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
	rows, err := d.db.QueryContext(ctx, querySQL, bucket, series, from.UnixMilli(), to.UnixMilli())
	if err != nil {
		return nil, err
	}
	return scanAll(rows, func(rows *sql.Rows) (core.Point, error) {
		var atMillis, seq int64
		var value float64
		err := rows.Scan(&atMillis, &seq, &value)
		return core.Point{At: time.UnixMilli(atMillis).UTC(), Value: value}, err
	})
}

// Tail reads at most limit of the most recent points for bucket/series within
// [from, to], ordered oldest to newest. It is the bounded read the spark path
// uses: pushing the LIMIT into SQL means a long-lived series costs the same to
// tail-read no matter how many rows it holds, rather than fetching the whole
// window and trimming in Go.
func (d *disk) Tail(bucket, series string, from, to time.Time, limit int) ([]core.Point, error) {
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	rows, err := d.db.QueryContext(ctx, tailSQL, bucket, series, from.UnixMilli(), to.UnixMilli(), limit)
	if err != nil {
		return nil, err
	}
	points, err := scanAll(rows, func(rows *sql.Rows) (core.Point, error) {
		var atMillis, seq int64
		var value float64
		err := rows.Scan(&atMillis, &seq, &value)
		return core.Point{At: time.UnixMilli(atMillis).UTC(), Value: value}, err
	})
	if err != nil {
		return nil, err
	}
	return reverse(points), nil
}

// reverse returns points in the opposite order, leaving the caller's window
// bound (DESC ... LIMIT, newest first) presented ascending like Query's, oldest
// first. A nil or single-element slice returns unchanged.
func reverse(points []core.Point) []core.Point {
	reversed := make([]core.Point, len(points))
	for i, p := range points {
		reversed[len(points)-1-i] = p
	}
	return reversed
}

func (d *disk) Minutes(bucket, series string, from, to time.Time) ([]core.Minute, error) {
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	rows, err := d.db.QueryContext(ctx, minutesSQL, bucket, series, from.Truncate(time.Minute).UnixMilli(), to.UnixMilli())
	if err != nil {
		return nil, err
	}
	return scanAll(rows, func(rows *sql.Rows) (core.Minute, error) {
		var minuteMillis int64
		var minute core.Minute
		err := rows.Scan(&minuteMillis, &minute.Min, &minute.Max, &minute.Avg)
		minute.At = time.UnixMilli(minuteMillis).UTC()
		return minute, err
	})
}

func (d *disk) Names(bucket string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	rows, err := d.db.QueryContext(ctx, namesSQL, bucket)
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

func (d *disk) Close() error {
	return d.db.Close()
}
