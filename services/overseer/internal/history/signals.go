package history

import (
	"altune/overseer/internal/core"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const signalsSchema = `
CREATE TABLE IF NOT EXISTS signals (
	seq INTEGER PRIMARY KEY AUTOINCREMENT,
	bucket TEXT NOT NULL,
	ring TEXT NOT NULL,
	at TEXT NOT NULL,
	kind TEXT NOT NULL,
	text TEXT NOT NULL,
	corr_id TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS signals_bucket_ring_seq ON signals (bucket, ring, seq);
`

const loadSignalsSQL = `
SELECT at, kind, text, corr_id FROM (
	SELECT seq, at, kind, text, corr_id FROM signals
	WHERE bucket = ?1 AND ring = ?2
	ORDER BY seq DESC
	LIMIT ?3
)
ORDER BY seq`

const trimSignalsSQL = `
DELETE FROM signals WHERE seq IN (
	SELECT seq FROM signals
	WHERE bucket = ? AND ring = ?
	ORDER BY seq DESC
	LIMIT -1 OFFSET ?
)`

type Signals interface {
	LoadSignals(ctx context.Context, bucket, ring string, limit int) ([]core.Signal, error)
	AppendSignals(bucket, ring string, capacity int, fresh []core.Signal)
}

type Database interface {
	Store
	Signals
}

func (d *disk) LoadSignals(ctx context.Context, bucket, ring string, limit int) ([]core.Signal, error) {
	rows, err := d.db.QueryContext(ctx, loadSignalsSQL, bucket, ring, max(limit, 0))
	if err != nil {
		return nil, err
	}
	return scanAll(rows, scanSignal)
}

func scanSignal(rows *sql.Rows) (core.Signal, error) {
	var at string
	var s core.Signal
	if err := rows.Scan(&at, &s.Kind, &s.Text, &s.CorrID); err != nil {
		return core.Signal{}, err
	}
	parsed, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return core.Signal{}, fmt.Errorf("signal at %q: %w", at, err)
	}
	s.At = parsed
	return s, nil
}

func (d *disk) AppendSignals(bucket, ring string, capacity int, fresh []core.Signal) {
	if len(fresh) == 0 {
		return
	}
	d.noteWrite(bucket, ring, d.appendSignals(bucket, ring, capacity, fresh))
}

func (d *disk) appendSignals(bucket, ring string, capacity int, fresh []core.Signal) error {
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	for _, s := range newest(fresh, capacity) {
		if _, err := tx.ExecContext(ctx, "INSERT INTO signals (bucket, ring, at, kind, text, corr_id) VALUES (?, ?, ?, ?, ?, ?)",
			bucket, ring, s.At.Format(time.RFC3339Nano), s.Kind, s.Text, s.CorrID); err != nil {
			return errors.Join(err, tx.Rollback())
		}
	}
	if _, err := tx.ExecContext(ctx, trimSignalsSQL, bucket, ring, capacity); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	return tx.Commit()
}

func newest(fresh []core.Signal, capacity int) []core.Signal {
	return fresh[len(fresh)-min(len(fresh), max(capacity, 0)):]
}

func (unavailable) LoadSignals(context.Context, string, string, int) ([]core.Signal, error) {
	return nil, nil
}

func (unavailable) AppendSignals(string, string, int, []core.Signal) {}
