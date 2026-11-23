package housekeeping_test

import (
	"altune/go-api/internal/app/housekeeping"
	"altune/go-api/internal/discovery"
	"context"
	"slices"
	"testing"
	"time"
)

type limitRecordingStore struct {
	discovery.FakeUnreleasedStore
	limits []int
}

func (l *limitRecordingStore) Due(_ context.Context, _ time.Time, limit int) ([]discovery.UnreleasedDueArtist, error) {
	l.limits = append(l.limits, limit)
	return nil, nil
}

func TestRefreshDueUnreleased_EachRunAsksTheStoreForAtMostTwoArtists(t *testing.T) {
	store := &limitRecordingStore{}
	svc := discovery.NewUnreleasedService(nil, nil, nil, discovery.UnreleasedConfig{}, discovery.WithUnreleasedStore(store))
	if err := housekeeping.RefreshDueUnreleased(svc)(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !slices.Equal(store.limits, []int{2}) {
		t.Errorf("Due limits = %v, want [2]", store.limits)
	}
}
