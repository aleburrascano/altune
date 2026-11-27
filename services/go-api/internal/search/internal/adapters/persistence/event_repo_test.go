package persistence_test

import (
	"altune/go-api/internal/search/internal/adapters/persistence"
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/clock/clocktest"
	"altune/go-api/internal/shared/dbtest"
	"altune/go-api/internal/shared/ids"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestPostgresEventStore_AppendAndRead(t *testing.T) {
	pool := dbtest.Pool(t)
	store := persistence.NewPostgresEventStore(pool, clock.Now)
	ctx := context.Background()
	userId := ids.NewUserId(uuid.New())

	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM discovery_events WHERE user_id = $1`, userId.UUID())
	})

	event := domain.InteractionEvent{
		UserId:    userId,
		Type:      domain.InteractionTypeSearchPerformed,
		QueryNorm: "kendrick lamar",
		Payload: map[string]any{
			"result_count": 12,
			"zero_result":  false,
		},
	}

	if err := store.Append(ctx, event); err != nil {
		t.Fatalf("Append: %v", err)
	}

	var (
		eventType string
		queryNorm *string
		payload   []byte
	)
	err := pool.QueryRow(ctx,
		`SELECT event_type, query_norm, payload FROM discovery_events WHERE user_id = $1`,
		userId.UUID(),
	).Scan(&eventType, &queryNorm, &payload)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}

	if eventType != "search_performed" {
		t.Errorf("event_type = %q, want search_performed", eventType)
	}
	if queryNorm == nil || *queryNorm != "kendrick lamar" {
		t.Errorf("query_norm = %v, want kendrick lamar", queryNorm)
	}

	var got map[string]any
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("payload unmarshal: %v", err)
	}
	if rc, ok := got["result_count"].(float64); !ok || rc != 12 {
		t.Errorf("payload result_count = %v, want 12", got["result_count"])
	}
}

func TestPostgresEventStore_NilPayloadAndQuery(t *testing.T) {
	pool := dbtest.Pool(t)
	store := persistence.NewPostgresEventStore(pool, clock.Now)
	ctx := context.Background()
	userId := ids.NewUserId(uuid.New())

	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM discovery_events WHERE user_id = $1`, userId.UUID())
	})

	event := domain.InteractionEvent{
		UserId: userId,
		Type:   domain.InteractionTypeWrongAlbum,
	}

	if err := store.Append(ctx, event); err != nil {
		t.Fatalf("Append: %v", err)
	}

	var (
		eventType string
		queryNorm *string
		payload   []byte
	)
	err := pool.QueryRow(ctx,
		`SELECT event_type, query_norm, payload FROM discovery_events WHERE user_id = $1`,
		userId.UUID(),
	).Scan(&eventType, &queryNorm, &payload)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}

	if eventType != "wrong_album" {
		t.Errorf("event_type = %q, want wrong_album", eventType)
	}
	if queryNorm != nil {
		t.Errorf("query_norm = %v, want NULL", *queryNorm)
	}
	if string(payload) != "{}" {
		t.Errorf("payload = %q, want {}", string(payload))
	}
}

func TestPostgresEventStore_Append_StampsAnEventWithoutATimeFromTheInjectedClock(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := dbtest.Pool(t)
	manual := clocktest.NewManual(time.Date(2026, time.March, 4, 10, 0, 0, 0, time.UTC))
	store := persistence.NewPostgresEventStore(pool, manual.Func())
	userId := ids.NewUserId(uuid.New())
	t.Cleanup(func() {
		_, _ = pool.Exec(context.WithoutCancel(t.Context()), `DELETE FROM discovery_events WHERE user_id = $1`, userId.UUID())
	})

	if err := store.Append(t.Context(), domain.InteractionEvent{UserId: userId, Type: domain.InteractionTypePlay}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	var occurredAt time.Time
	if err := pool.QueryRow(t.Context(), `SELECT occurred_at FROM discovery_events WHERE user_id = $1`, userId.UUID()).Scan(&occurredAt); err != nil {
		t.Fatalf("read occurred_at: %v", err)
	}
	if want := manual.Now(); !occurredAt.Equal(want) {
		t.Errorf("occurred_at = %v, want %v from the injected clock", occurredAt, want)
	}
}

func TestNewPostgresEventStore_PanicsOnANilClock(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("NewPostgresEventStore without a clock did not panic")
		}
	}()

	persistence.NewPostgresEventStore(nil, nil)
}
