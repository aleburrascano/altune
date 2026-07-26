package service

import (
	"context"
	"errors"
	"testing"

	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"

	"github.com/google/uuid"
)

type stubResolver struct {
	identity ports.RecordingIdentity
	err      error
	queries  []ports.RecordingQuery
}

func (r *stubResolver) Resolve(_ context.Context, q ports.RecordingQuery) (ports.RecordingIdentity, error) {
	r.queries = append(r.queries, q)
	return r.identity, r.err
}

func serviceWithResolver(r ports.RecordingResolver) *AcquireTrackAudioService {
	return NewAcquireTrackAudioService(
		newFakeTrackRepository(), fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore(),
		WithRecordingResolver(r),
	)
}

func TestResolveIdentity_SuppliesMissingDuration(t *testing.T) {
	svc := serviceWithResolver(&stubResolver{
		identity: ports.RecordingIdentity{Duration: 181, ISRC: "USUM71922973"},
	})
	ac := &AcquisitionContext{Track: TrackRef{Title: "Smaxk Or Die", Artist: "Fatt Smaxk"}}

	svc.resolveIdentity(context.Background(), ac)

	if ac.Track.Duration != 181 {
		t.Errorf("duration = %v, want the resolved 181", ac.Track.Duration)
	}
	if ac.Track.ISRC != "USUM71922973" {
		t.Errorf("isrc = %q, want the resolved one", ac.Track.ISRC)
	}
	if ac.Identity.Duration != 181 {
		t.Errorf("identity not recorded on the context: %+v", ac.Identity)
	}
}

func TestResolveIdentity_NeverOverwritesSavedDuration(t *testing.T) {
	svc := serviceWithResolver(&stubResolver{identity: ports.RecordingIdentity{Duration: 999}})
	ac := &AcquisitionContext{Track: TrackRef{Title: "T", Artist: "A", Duration: 200}}

	svc.resolveIdentity(context.Background(), ac)

	if ac.Track.Duration != 200 {
		t.Errorf("duration = %v, want the saved 200 kept", ac.Track.Duration)
	}
}

func TestResolveIdentity_ResolverErrorIsNonFatal(t *testing.T) {
	svc := serviceWithResolver(&stubResolver{err: errors.New("discovery down")})
	ac := &AcquisitionContext{Track: TrackRef{Title: "T", Artist: "A", Duration: 200}}

	svc.resolveIdentity(context.Background(), ac)

	if ac.Track.Duration != 200 || !ac.Identity.IsZero() {
		t.Errorf("a resolver failure must leave the track untouched, got %+v", ac.Track)
	}
}

func TestResolveIdentity_PassesTrackMetadataToResolver(t *testing.T) {
	stub := &stubResolver{}
	svc := serviceWithResolver(stub)
	ac := &AcquisitionContext{Track: TrackRef{
		Title: "Circles", Artist: "Post Malone", Album: "Hollywood's Bleeding", ISRC: "X",
	}}

	svc.resolveIdentity(context.Background(), ac)

	if len(stub.queries) != 1 {
		t.Fatalf("resolver calls = %d, want 1", len(stub.queries))
	}
	got := stub.queries[0]
	want := ports.RecordingQuery{Title: "Circles", Artist: "Post Malone", Album: "Hollywood's Bleeding", ISRC: "X"}
	if got != want {
		t.Errorf("query = %+v, want %+v", got, want)
	}
}

func TestExecute_WithoutResolverStillRuns(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	repo := newFakeTrackRepository()
	track, err := domain.NewTrack(userId, "Song", "Artist", "Album")
	if err != nil {
		t.Fatalf("new track: %v", err)
	}
	repo.tracks[track.ID.String()+":"+userId.String()] = track

	svc := NewAcquireTrackAudioService(repo, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())

	_ = svc.Execute(context.Background(), userId, track.ID)
}

func TestRecordingIdentity_SourceFor(t *testing.T) {
	id := ports.RecordingIdentity{Sources: []ports.RecordingSource{
		{Provider: "deezer", ExternalID: "123"},
		{Provider: "youtube", ExternalID: "abc"},
	}}

	if got, ok := id.SourceFor("youtube"); !ok || got.ExternalID != "abc" {
		t.Errorf("SourceFor(youtube) = %+v %v", got, ok)
	}
	if _, ok := id.SourceFor("tidal"); ok {
		t.Error("SourceFor(tidal) should report missing")
	}
}
