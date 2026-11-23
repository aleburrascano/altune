package service_test

import (
	"altune/go-api/internal/acquisition/internal/adapters/acquisitiontest"
	"altune/go-api/internal/acquisition/internal/adapters/tempfs"
	"altune/go-api/internal/acquisition/internal/core/grade"
	"altune/go-api/internal/acquisition/internal/core/ports"
	"altune/go-api/internal/acquisition/service"
	"altune/go-api/internal/acquisition/service/upgrade"
	"altune/go-api/internal/shared/events"
	shared "altune/go-api/internal/shared/kernel"
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
)

func TestExecute_MarksTheTrackReadyOnTheFileItStored(t *testing.T) {
	track := pendingBlindingLights()
	ledger := acquisitiontest.NewLedger(acquisitiontest.LedgerTrack{TrackSnapshot: track})
	store := &objectStore{objects: map[string]string{}}
	svc := service.NewAcquireTrackAudioService(ledger, service.NewSourceRegistry(freshAudioSource{}), store,
		service.WithAcquireTempWorkspace(tempfs.New()))

	err := svc.Execute(context.Background(), track.UserId, track.ID)

	requireReady(t, ledger, track.ID, err)
	want := service.BuildAudioRef(service.TrackRef{
		ID: track.ID.String(), UserID: track.UserId.String(), Title: track.Title, Artist: track.Artist, Album: track.Album,
	}, "audio.mp3")
	if got, _ := ledger.Track(track.ID); got.AudioRef == nil || *got.AudioRef != want {
		t.Fatalf("audio ref = %v, want %q", got.AudioRef, want)
	}
	if !store.holds(want) {
		t.Errorf("the store does not hold %q, the file the track now points at", want)
	}
}

func TestExecuteReplace_SwapsTheServingFileForTheOneItStored(t *testing.T) {
	served := seedReadyTrack(t)

	if err := served.service().ExecuteReplace(context.Background(), served.userID, served.trackID); err != nil {
		t.Fatalf("ExecuteReplace: %v", err)
	}

	got, _ := served.ledger.Track(served.trackID)
	if got.Status != ports.TrackReady || got.AudioRef == nil || *got.AudioRef == served.originalRef {
		t.Fatalf("track = %v on %v, want ready on a new file, not the original %q", got.Status, got.AudioRef, served.originalRef)
	}
	if !served.store.holds(*got.AudioRef) {
		t.Errorf("the store does not hold %q, the file the track now serves", *got.AudioRef)
	}
}

type vanishingLedger struct{ *acquisitiontest.Ledger }

func (vanishingLedger) Complete(context.Context, shared.TrackId, shared.UserId, ports.Fence, ports.Completion) error {
	return ports.ErrTrackNotFound
}

func TestExecute_ATrackGoneAtTheCommitFailsTheRunAsAcquisitionFailed(t *testing.T) {
	track := pendingBlindingLights()
	ledger := acquisitiontest.NewLedger(acquisitiontest.LedgerTrack{TrackSnapshot: track})
	svc := service.NewAcquireTrackAudioService(vanishingLedger{ledger}, service.NewSourceRegistry(freshAudioSource{}),
		&objectStore{objects: map[string]string{}}, service.WithAcquireTempWorkspace(tempfs.New()))

	err := svc.Execute(context.Background(), track.UserId, track.ID)

	if err == nil || !strings.Contains(err.Error(), "track not found for update") {
		t.Fatalf("Execute = %v, want the update step's track-not-found failure", err)
	}
	if code := persistedCode(t, ledger, track.ID); code != shared.FailureAcquisitionFailed {
		t.Errorf("persisted failure code = %q, want %q: failure_reason is stored and shown to the client", code, shared.FailureAcquisitionFailed)
	}
}

type progressRecorder struct {
	mu     sync.Mutex
	stages []string
}

func (p *progressRecorder) Publish(_ context.Context, _ shared.UserId, eventType events.EventType, payload map[string]any) {
	if eventType != events.TypeTrackAcquisitionProgress {
		return
	}
	stage, _ := payload["stage"].(string)
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stages = append(p.stages, stage)
}

func (p *progressRecorder) published() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.stages)
}

func TestScheduledRun_TellsTheClientWhenItReachesTheUpdateTrackStage(t *testing.T) {
	track := acquisitiontest.LedgerTrack{TrackSnapshot: pendingBlindingLights()}
	ledger := acquisitiontest.NewLedger(track)
	svc := service.NewAcquireTrackAudioService(ledger, service.NewSourceRegistry(freshAudioSource{}),
		&objectStore{objects: map[string]string{}}, service.WithAcquireTempWorkspace(tempfs.New()))
	progress := &progressRecorder{}

	outcome := scheduleOnce(t, svc, track, nil, service.WithSchedulerEvents(progress))

	if !outcome.settled {
		t.Fatalf("job outcome = %+v, want the run settled", outcome)
	}
	if stages := progress.published(); !slices.Contains(stages, "update_track") {
		t.Errorf("progress stages = %v, want %q among them: the mobile client renders this name", stages, "update_track")
	}
}

func TestReplaceGated_AnUnprobedFileGetsNoLengthRescue(t *testing.T) {
	const recording = "mb-blinding"
	served := seedReadyTrack(t)
	source := &offeringSource{found: []ports.AudioCandidate{{
		Title: "The Weeknd - Blinding Lights", URL: "https://www.youtube.com/watch?v=fanUploadAA",
		Channel: "Some Fan", Kind: ports.KindUpload, Duration: 285, Categories: []string{"Music"},
	}}}
	svc := service.NewAcquireTrackAudioService(served.ledger, service.NewSourceRegistry(source), served.store,
		service.WithAcquireTempWorkspace(tempfs.New()),
		service.WithRecordingResolver(&resolver{identity: ports.RecordingIdentity{MBID: recording, MBIDs: []string{recording}}}),
		service.WithAudioIdentifier(&identifier{match: ports.RecordingMatch{AcoustID: "ac-1", Results: []ports.AcoustIDResult{
			{Score: 0.97, Recordings: []ports.LinkedRecording{{MBID: recording}}},
		}}}))
	snapshot, err := served.ledger.Get(context.Background(), served.trackID, served.userID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	gate := upgrade.Gate{Current: grade.B, CatalogSeconds: 285, StoredSeconds: 231}

	_, err = svc.ReplaceGated(context.Background(), served.userID, snapshot, gate)

	if !errors.Is(err, upgrade.ErrNotAnUpgrade) {
		t.Fatalf("ReplaceGated = %v, want ErrNotAnUpgrade: the source's claimed 285 s was never measured", err)
	}
	if !source.tried("https://www.youtube.com/watch?v=fanUploadAA") {
		t.Fatalf("the fan upload was never fetched, so the gate never judged it; tried %v", source.fetches())
	}
	if got, _ := served.ledger.Track(served.trackID); got.AudioRef == nil || *got.AudioRef != served.originalRef {
		t.Errorf("audio ref = %v, want the original %q kept", got.AudioRef, served.originalRef)
	}
}
