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

type PgxIdentityStore struct {
	pool *pgxpool.Pool
}

func NewPgxIdentityStore(pool *pgxpool.Pool) *PgxIdentityStore {
	return &PgxIdentityStore{pool: pool}
}

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
