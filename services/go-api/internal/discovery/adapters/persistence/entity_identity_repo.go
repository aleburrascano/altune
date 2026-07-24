package persistence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var _ ports.IdentityStore = (*PgxIdentityStore)(nil)

// PgxIdentityStore persists the durable reverse identity map (entity_identity):
// (provider, external_id, kind) → MBID + bridged xref. It is the source of truth
// behind the IdentityStore port; a Redis read-through fronts it in production.
type PgxIdentityStore struct {
	pool *pgxpool.Pool
}

func NewPgxIdentityStore(pool *pgxpool.Pool) *PgxIdentityStore {
	return &PgxIdentityStore{pool: pool}
}

// PersistBridges upserts one row per (provider, external_id) in xref, all pointing
// at mbid and carrying the full xref blob. A no-op when there is nothing to bridge.
func (s *PgxIdentityStore) PersistBridges(
	ctx context.Context,
	kind domain.ResultKind,
	mbid string,
	xref map[string]string,
) error {
	if mbid == "" || len(xref) == 0 {
		return nil
	}
	blob, err := json.Marshal(xref)
	if err != nil {
		return fmt.Errorf("marshal xref: %w", err)
	}

	batch := &pgx.Batch{}
	for provider, externalID := range xref {
		if provider == "" || externalID == "" {
			continue
		}
		// xref merges (|| keeps old keys, new values win per key) so previously
		// learned edges survive a partial re-learn — a later pass that bridged
		// only {deezer} must not erase the {spotify, discogs} edges an earlier
		// pass recorded. mbid stays last-write-wins; whether a CONFLICTING mbid
		// should instead be rejected/flagged is an open question.
		batch.Queue(
			`INSERT INTO entity_identity (provider, external_id, kind, mbid, xref)
			 VALUES ($1, $2, $3, $4, $5)
			 ON CONFLICT (provider, external_id, kind)
			 DO UPDATE SET mbid = EXCLUDED.mbid,
				xref = entity_identity.xref || EXCLUDED.xref,
				resolved_at = now()`,
			provider, externalID, kind.String(), mbid, blob,
		)
	}
	if batch.Len() == 0 {
		return nil
	}

	br := s.pool.SendBatch(ctx, batch)
	defer br.Close()
	for range batch.Len() {
		if _, err := br.Exec(); err != nil {
			return fmt.Errorf("persist identity bridge: %w", err)
		}
	}
	return nil
}

// LookupByProviderID returns the bridged identity for one provider id, or
// ok=false when none was recorded. A miss is not an error.
func (s *PgxIdentityStore) LookupByProviderID(
	ctx context.Context,
	kind domain.ResultKind,
	provider, externalID string,
) (string, map[string]string, bool) {
	if provider == "" || externalID == "" {
		return "", nil, false
	}
	var mbid string
	var xrefBlob []byte
	err := s.pool.QueryRow(ctx,
		`SELECT mbid, xref FROM entity_identity
		 WHERE provider = $1 AND external_id = $2 AND kind = $3`,
		provider, externalID, kind.String(),
	).Scan(&mbid, &xrefBlob)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil, false
	}
	if err != nil {
		// A lookup failure must never break the search path; degrade to a miss —
		// but log it, so a real DB problem isn't indistinguishable from a miss.
		slog.DebugContext(ctx, "identity.lookup_failed",
			"kind", kind.String(), "provider", provider, "external_id", externalID, "error", err)
		return "", nil, false
	}

	xref := map[string]string{}
	if len(xrefBlob) > 0 {
		_ = json.Unmarshal(xrefBlob, &xref)
	}
	return mbid, xref, mbid != ""
}

// Invalidate deletes one recorded identity row. Deleting a row that does not
// exist is a no-op, not an error. Purge/remediation surface only — see the port.
func (s *PgxIdentityStore) Invalidate(
	ctx context.Context,
	kind domain.ResultKind,
	provider, externalID string,
) error {
	if provider == "" || externalID == "" {
		return nil
	}
	_, err := s.pool.Exec(ctx,
		`DELETE FROM entity_identity
		 WHERE provider = $1 AND external_id = $2 AND kind = $3`,
		provider, externalID, kind.String(),
	)
	if err != nil {
		return fmt.Errorf("invalidate identity: %w", err)
	}
	return nil
}
