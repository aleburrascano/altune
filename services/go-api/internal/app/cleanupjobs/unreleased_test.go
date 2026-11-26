package cleanupjobs_test

import (
	"altune/go-api/internal/app/cleanupjobs"
	"altune/go-api/internal/search"
	"context"
	"slices"
	"testing"
	"time"
)

type limitRecordingStore struct {
	search.FakeUnreleasedStore
	limits []int
}

func (l *limitRecordingStore) Due(_ context.Context, _ time.Time, limit int) ([]search.UnreleasedDueArtist, error) {
	l.limits = append(l.limits, limit)
	return nil, nil
}

func TestRefreshDueUnreleased_EachRunAsksTheStoreForAtMostTwoArtists(t *testing.T) {
	store := &limitRecordingStore{}
	svc := search.NewUnreleasedService(nil, nil, nil, search.UnreleasedConfig{}, search.WithUnreleasedStore(store))
	if err := cleanupjobs.RefreshDueUnreleased(svc)(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !slices.Equal(store.limits, []int{2}) {
		t.Errorf("Due limits = %v, want [2]", store.limits)
	}
}
