package persistence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	_ ports.IdentityStore       = (*PgxIdentityStore)(nil)
	_ ports.BatchIdentityLookup = (*PgxIdentityStore)(nil)
)

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

	batch := bridgeBatch(kind, mbid, xref, blob)
	if batch.Len() == 0 {
		return nil
	}

	br := s.pool.SendBatch(ctx, batch)
	for range batch.Len() {
		if _, err := br.Exec(); err != nil {
			return errors.Join(fmt.Errorf("persist identity bridge: %w", err), br.Close())
		}
	}
	if err := br.Close(); err != nil {
		return fmt.Errorf("close identity bridge batch: %w", err)
	}
	return nil
}

func bridgeBatch(kind domain.ResultKind, mbid string, xref map[string]string, blob []byte) *pgx.Batch {
	batch := &pgx.Batch{}
	for _, provider := range slices.Sorted(maps.Keys(xref)) {
		externalID := xref[provider]
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
	return batch
}

func (s *PgxIdentityStore) LookupByProviderID(
	ctx context.Context,
	kind domain.ResultKind,
	provider domain.ProviderKey, externalID string,
) (string, map[string]string, bool) {
	if provider == "" || externalID == "" {
		return "", nil, false
	}
	var mbid string
	var xrefBlob []byte
	err := s.pool.QueryRow(ctx,
		`SELECT mbid, xref FROM entity_identity
		 WHERE provider = $1 AND external_id = $2 AND kind = $3`,
		provider.String(), externalID, kind.String(),
	).Scan(&mbid, &xrefBlob)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil, false
	}
	if err != nil {
		slog.WarnContext(ctx, "identity.lookup_failed",
			"kind", kind.String(), "provider", provider.String(), "external_id", externalID, "error", err)
		return "", nil, false
	}

	return mbid, decodeXref(ctx, mbid, xrefBlob), mbid != ""
}

func (s *PgxIdentityStore) LookupByProviderIDs(
	ctx context.Context,
	refs []ports.IdentityRef,
) map[ports.IdentityRef]ports.IdentityHit {
	requested := identityRefsByRow(refs)
	if len(requested) == 0 {
		return nil
	}
	hits, err := s.queryIdentityHits(ctx, requested)
	if err != nil {
		slog.WarnContext(ctx, "identity.batch_lookup_failed", "refs", len(requested), "error", err)
		return nil
	}
	return hits
}

type identityRow [3]string

func identityRefsByRow(refs []ports.IdentityRef) map[identityRow]ports.IdentityRef {
	requested := make(map[identityRow]ports.IdentityRef, len(refs))
	for _, ref := range refs {
		if ref.Provider == "" || ref.ExternalID == "" {
			continue
		}
		requested[identityRow{ref.Provider.String(), ref.ExternalID, ref.Kind.String()}] = ref
	}
	return requested
}

func (s *PgxIdentityStore) queryIdentityHits(
	ctx context.Context,
	requested map[identityRow]ports.IdentityRef,
) (map[ports.IdentityRef]ports.IdentityHit, error) {
	var providers, externalIDs, kinds []string
	for row := range requested {
		providers, externalIDs, kinds = append(providers, row[0]), append(externalIDs, row[1]), append(kinds, row[2])
	}
	rows, err := s.pool.Query(ctx,
		`SELECT provider, external_id, kind, mbid, xref FROM entity_identity
		 WHERE (provider, external_id, kind) IN (
			SELECT * FROM unnest($1::text[], $2::text[], $3::text[]))`,
		providers, externalIDs, kinds,
	)
	if err != nil {
		return nil, fmt.Errorf("batch lookup identity: %w", err)
	}
	return scanIdentityHits(ctx, rows, requested)
}

func scanIdentityHits(ctx context.Context, rows pgx.Rows, requested map[identityRow]ports.IdentityRef) (map[ports.IdentityRef]ports.IdentityHit, error) {
	defer rows.Close()
	hits := make(map[ports.IdentityRef]ports.IdentityHit, len(requested))
	for rows.Next() {
		var row identityRow
		var mbid string
		var xrefBlob []byte
		if err := rows.Scan(&row[0], &row[1], &row[2], &mbid, &xrefBlob); err != nil {
			return nil, fmt.Errorf("scan identity: %w", err)
		}
		ref, ok := requested[row]
		if !ok || mbid == "" {
			continue
		}
		hits[ref] = ports.IdentityHit{MBID: mbid, Xref: decodeXref(ctx, mbid, xrefBlob)}
	}
	return hits, rows.Err()
}

func decodeXref(ctx context.Context, mbid string, blob []byte) map[string]string {
	xref := map[string]string{}
	if len(blob) == 0 {
		return xref
	}
	if err := json.Unmarshal(blob, &xref); err != nil {
		slog.WarnContext(ctx, "identity.xref_corrupt", "mbid", mbid, "error", err)
		return map[string]string{}
	}
	return xref
}

func (s *PgxIdentityStore) Invalidate(
	ctx context.Context,
	kind domain.ResultKind,
	provider domain.ProviderKey, externalID string,
) error {
	if provider == "" || externalID == "" {
		return nil
	}
	_, err := s.pool.Exec(ctx,
		`DELETE FROM entity_identity
		 WHERE provider = $1 AND external_id = $2 AND kind = $3`,
		provider.String(), externalID, kind.String(),
	)
	if err != nil {
		return fmt.Errorf("invalidate identity: %w", err)
	}
	return nil
}
