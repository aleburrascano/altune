package persistence

import (
	"context"
	"encoding/json"
	"testing"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared"

	"github.com/google/uuid"
)

func TestPgxEventStore_AppendAndRead(t *testing.T) {
	pool := testPool(t)
	store := NewPgxEventStore(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM discovery_events WHERE user_id = $1`, userId.UUID())
	})

	event := domain.InteractionEvent{
		UserId:    userId,
		Type:      domain.EventTypeSearchPerformed,
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

// A non-search event has no query and no payload: query_norm must persist as
// NULL and payload must default to an empty JSON object, not error.
func TestPgxEventStore_NilPayloadAndQuery(t *testing.T) {
	pool := testPool(t)
	store := NewPgxEventStore(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM discovery_events WHERE user_id = $1`, userId.UUID())
	})

	event := domain.InteractionEvent{
		UserId: userId,
		Type:   domain.EventTypeWrongAlbum,
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
