package handler

import (
	"altune/go-api/internal/admin/eventtap"
	"altune/go-api/internal/shared"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/google/uuid"
)

type nopPublisher struct{}

func (nopPublisher) Publish(context.Context, shared.UserId, string, map[string]any) {}

func TestEventFrameOmitsRawUserAndSearchText(t *testing.T) {
	tap := eventtap.New(nopPublisher{})
	ch, cancel, err := tap.SubscribeAll()
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	uid := shared.NewUserId(uuid.New())
	tap.Publish(context.Background(), uid, "search", map[string]any{"query": "my private query"})
	frame, ok := dataFrame(<-ch)
	if !ok {
		t.Fatal("frame not sendable")
	}
	if strings.Contains(frame, "my private query") || strings.Contains(frame, uid.String()) {
		t.Fatalf("frame leaks: %s", frame)
	}
	sum := sha256.Sum256([]byte(uid.String()))
	want := `"user":"` + hex.EncodeToString(sum[:4]) + `"`
	if !strings.Contains(frame, want) {
		t.Fatalf("frame lacks digest: %s", frame)
	}
}
