package app

import (
	"altune/go-api/internal/shared"
	"context"
	"testing"

	catalogDomain "altune/go-api/internal/catalog/domain"
	catalogService "altune/go-api/internal/catalog/service"

	"github.com/google/uuid"
)

type recordingFiller struct {
	setCalls int
}

func (f *recordingFiller) SetTrackNumber(context.Context, catalogDomain.TrackId, shared.UserId, int) (bool, error) {
	f.setCalls++
	return true, nil
}

func (f *recordingFiller) GetByID(context.Context, catalogDomain.TrackId, shared.UserId) (*catalogDomain.Track, error) {
	return nil, nil
}

func TestCatalogTrackNumberSetter_SurfacesMalformedId(t *testing.T) {
	filler := &recordingFiller{}
	setter := catalogTrackNumberSetter{svc: catalogService.NewSetTrackNumberService(filler)}

	_, err := setter.Execute(context.Background(), shared.NewUserId(uuid.New()), "not-a-uuid", 3)

	if err == nil {
		t.Fatal("expected an error for a malformed track ID")
	}
	if filler.setCalls != 0 {
		t.Errorf("SetTrackNumber called %d times, want 0", filler.setCalls)
	}
}
