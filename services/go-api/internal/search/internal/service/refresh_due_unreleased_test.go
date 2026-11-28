package service_test

import (
	"altune/go-api/internal/search/internal/adapters/memory"
	"altune/go-api/internal/search/internal/service"
	"context"
	"slices"
	"testing"
	"time"
)

func TestRefreshDueUnreleased_AsksForWhatIsDueAtTheClocksInstantInUTC(t *testing.T) {
	store := &dueStore{}
	svc := refreshService(t, &memory.GeniusSongCatalog{}, store)

	if err := service.RefreshDueUnreleased(svc, atTestNow)(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}

	if !store.gotNow.Equal(testNow) || store.gotNow.Location() != time.UTC {
		t.Errorf("Due was asked as of %v, want %v in UTC", store.gotNow, testNow.UTC())
	}
}

func TestRefreshDueUnreleased_EachRunAsksTheStoreForAtMostTwoArtists(t *testing.T) {
	store := &dueStore{}
	svc := refreshService(t, &memory.GeniusSongCatalog{}, store)

	if err := service.RefreshDueUnreleased(svc, atTestNow)(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}

	if !slices.Equal([]int{store.gotLimit}, []int{2}) {
		t.Errorf("Due limit = %d, want 2", store.gotLimit)
	}
}
