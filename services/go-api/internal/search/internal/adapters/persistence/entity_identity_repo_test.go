package persistence_test

import (
	"altune/go-api/internal/search/internal/adapters/persistence"
	"altune/go-api/internal/search/internal/core/ports"
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/shared/logging"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresIdentityStore_EmptyInputGuards(t *testing.T) {
	store := persistence.NewPostgresIdentityStore(nil)
	ctx := context.Background()

	t.Run("LookupByProviderID misses on empty inputs", func(t *testing.T) {
		if _, _, ok := store.LookupByProviderID(ctx, domain.ResultKindArtist, "", "123"); ok {
			t.Error("empty provider: got hit, want miss")
		}
		if _, _, ok := store.LookupByProviderID(ctx, domain.ResultKindArtist, "deezer", ""); ok {
			t.Error("empty external id: got hit, want miss")
		}
	})

	t.Run("LookupByProviderIDs skips the query when every ref is blank", func(t *testing.T) {
		refs := []ports.IdentityRef{{Kind: domain.ResultKindArtist, ExternalID: "123"}, {Kind: domain.ResultKindArtist, Provider: "deezer"}}
		if hits := store.LookupByProviderIDs(ctx, refs); len(hits) != 0 {
			t.Errorf("blank refs: hits = %v, want none", hits)
		}
		if hits := store.LookupByProviderIDs(ctx, nil); len(hits) != 0 {
			t.Errorf("nil refs: hits = %v, want none", hits)
		}
	})
}

func captureWarnings(t *testing.T) *logging.Capture {
	t.Helper()
	return logging.CaptureText(t, slog.LevelWarn)
}

func TestPostgresIdentityStore_LookupByProviderID_DBFailureWarnsAndMisses(t *testing.T) {
	logs := captureWarnings(t)
	pool, err := pgxpool.New(context.Background(), "postgres://u:p@127.0.0.1:1/db?connect_timeout=1")
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	defer pool.Close()

	_, _, ok := persistence.NewPostgresIdentityStore(pool).LookupByProviderID(context.Background(), domain.ResultKindArtist, "deezer", "1")

	if ok {
		t.Error("db failure: got hit, want miss")
	}
	if !strings.Contains(logs.String(), "identity.lookup_failed") {
		t.Errorf("db failure not logged at Warn: %q", logs.String())
	}
}
