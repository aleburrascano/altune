package service

import (
	"altune/go-api/internal/catalog/catalogtest"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"context"
	"testing"
)

// Each stub implements only the track methods one service calls, so this file
// compiles only while every service depends on its narrow track port.

type stubAdder struct{}

func (stubAdder) Add(context.Context, *domain.Track) (*domain.Track, bool, error) {
	return nil, false, nil
}

type stubGetter struct{ track *domain.Track }

func (g stubGetter) GetByID(context.Context, domain.TrackId, shared.UserId) (*domain.Track, error) {
	return g.track, nil
}

type stubBatchGetter struct{}

func (stubBatchGetter) ListByIDs(context.Context, shared.UserId, []domain.TrackId) ([]*domain.Track, error) {
	return nil, nil
}

type stubLister struct{}

func (stubLister) ListForUser(context.Context, shared.UserId, int, int) ([]*domain.Track, int, error) {
	return nil, 0, nil
}

type stubCounter struct{ held int }

func (c stubCounter) CountForUser(context.Context, shared.UserId, int) (int, error) {
	return c.held, nil
}

type stubUpdater struct{}

func (stubUpdater) Update(context.Context, *domain.Track, int) error { return nil }

type stubNumberSetter struct{}

func (stubNumberSetter) SetTrackNumber(context.Context, domain.TrackId, shared.UserId, int) (bool, error) {
	return true, nil
}

type stubDeleter struct{ deleted bool }

func (d stubDeleter) Delete(context.Context, domain.TrackId, shared.UserId) (bool, *string, error) {
	return d.deleted, nil, nil
}

type stubAudioRefLookup struct{}

func (stubAudioRefLookup) AudioRefInUse(context.Context, string, domain.TrackId) (bool, error) {
	return false, nil
}

func TestServicesDependOnNarrowTrackPorts(t *testing.T) {
	ctx := context.Background()
	userId := testUserId()
	store := catalogtest.NewAudioStore()
	want := &domain.Track{Title: "Narrow"}

	got, err := NewGetTrackStatusService(stubGetter{track: want}).Execute(ctx, userId, domain.NewTrackId())
	if err != nil || got != want {
		t.Fatalf("GetTrackStatus via getter-only port = (%v, %v), want the stub's track", got, err)
	}
	if err := NewDeleteTrackService(struct {
		stubDeleter
		stubAudioRefLookup
	}{stubDeleter: stubDeleter{deleted: true}}, store).Execute(ctx, userId, domain.NewTrackId()); err != nil {
		t.Fatalf("DeleteTrack via deleter-only port: %v", err)
	}
	if _, err := NewSetTrackNumberService(struct {
		stubGetter
		stubNumberSetter
	}{}).Execute(ctx, userId, domain.NewTrackId(), 7); err != nil {
		t.Fatalf("SetTrackNumber via setter-only port: %v", err)
	}

	_ = NewAddTrackService(struct {
		stubAdder
		stubCounter
		stubUpdater
	}{})
	_ = NewAudioURLService(stubBatchGetter{}, store)
	_ = NewBackfillFeaturedService(stubLister{}, nil, nil)
	_ = NewStreamTrackService(struct {
		stubGetter
		stubUpdater
	}{}, store)
	_ = NewPlaylistMembershipService(catalogtest.NewPlaylistRepo(), struct {
		stubGetter
		stubBatchGetter
	}{})
}
