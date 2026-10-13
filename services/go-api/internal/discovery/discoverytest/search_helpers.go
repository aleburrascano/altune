package discoverytest

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared"
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

type FakeProvider struct {
	Provider domain.ProviderName
	Results  []domain.SearchResult
	Err      error
	Delay    time.Duration
}

func (p *FakeProvider) Name() domain.ProviderName { return p.Provider }

func (p *FakeProvider) Search(ctx context.Context, _ string, _ map[domain.ResultKind]bool) ([]domain.SearchResult, error) {
	if p.Delay > 0 {
		select {
		case <-time.After(p.Delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return p.Results, p.Err
}

func (p *FakeProvider) SupportedKinds() map[domain.ResultKind]bool {
	return map[domain.ResultKind]bool{
		domain.ResultKindTrack:  true,
		domain.ResultKindAlbum:  true,
		domain.ResultKindArtist: true,
	}
}

func NewQuery(t *testing.T, raw string) *domain.SearchQuery {
	t.Helper()
	q, err := domain.NewSearchQuery(raw, map[domain.ResultKind]bool{
		domain.ResultKindTrack:  true,
		domain.ResultKindAlbum:  true,
		domain.ResultKindArtist: true,
	}, 20)
	if err != nil {
		t.Fatal(err)
	}
	return q
}

func NewUser() shared.UserId {
	return shared.NewUserId(uuid.New())
}

type PanickingEventStore struct{ Calls int32 }

func (p *PanickingEventStore) Append(context.Context, domain.InteractionEvent) error {
	atomic.AddInt32(&p.Calls, 1)
	panic("telemetry adapter bug")
}
