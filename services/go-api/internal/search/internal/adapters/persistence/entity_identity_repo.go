package persistence

import (
	"altune/go-api/internal/search/domain"
	"altune/go-api/internal/search/internal/core/ports"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	_ ports.IdentityStore       = (*PostgresIdentityStore)(nil)
	_ ports.BatchIdentityLookup = (*PostgresIdentityStore)(nil)
)

type PostgresIdentityStore struct {
	pool *pgxpool.Pool
}

func NewPostgresIdentityStore(pool *pgxpool.Pool) *PostgresIdentityStore {
	return &PostgresIdentityStore{pool: pool}
}

func (s *PostgresIdentityStore) LookupByProviderID(
	ctx context.Context,
	kind domain.ResultKind,
	provider domain.ProviderKey, externalID string,
) (string, map[domain.ProviderKey]string, bool) {
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

func (s *PostgresIdentityStore) LookupByProviderIDs(
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

func (s *PostgresIdentityStore) queryIdentityHits(
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
	stored, err := pgx.CollectRows(rows, pgx.RowToStructByName[storedIdentity])
	if err != nil {
		return nil, fmt.Errorf("scan identity: %w", err)
	}
	hits := make(map[ports.IdentityRef]ports.IdentityHit, len(requested))
	for _, s := range stored {
		ref, ok := requested[identityRow{s.Provider, s.ExternalID, s.Kind}]
		if !ok || s.MBID == "" {
			continue
		}
		hits[ref] = ports.IdentityHit{MBID: s.MBID, Xref: decodeXref(ctx, s.MBID, s.Xref)}
	}
	return hits, nil
}

type storedIdentity struct {
	Provider   string
	ExternalID string
	Kind       string
	MBID       string
	Xref       []byte
}

func decodeXref(ctx context.Context, mbid string, blob []byte) map[domain.ProviderKey]string {
	xref := map[domain.ProviderKey]string{}
	if len(blob) == 0 {
		return xref
	}
	if err := json.Unmarshal(blob, &xref); err != nil {
		slog.WarnContext(ctx, "identity.xref_corrupt", "mbid", mbid, "error", err)
		return map[domain.ProviderKey]string{}
	}
	return xref
}
