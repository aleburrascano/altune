package service

import (
	"altune/go-api/internal/acquisition/adapters/catalogbridge"
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/events"
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
)

type recordingReporter struct {
	stages  []string
	srcURL  string
	prov    string
	timings map[string]time.Duration
	format  ports.SourceFormat
	file    ports.StoredFile
}

func (r *recordingReporter) meta(string, string, string)               {}
func (r *recordingReporter) stage(n string)                            { r.stages = append(r.stages, n) }
func (r *recordingReporter) source(u string)                           { r.srcURL = u }
func (r *recordingReporter) provenance(p string)                       { r.prov = p }
func (r *recordingReporter) fetched(f ports.SourceFormat)              { r.format = f }
func (r *recordingReporter) decided(_ float64, _ string, _, _ float64) {}
func (r *recordingReporter) stored(f ports.StoredFile)                 { r.file = f }
func (r *recordingReporter) timing(stage string, d time.Duration) {
	if r.timings == nil {
		r.timings = map[string]time.Duration{}
	}
	r.timings[stage] += d
}

func TestRunPipeline_ReportsStageAndSource(t *testing.T) {
	rep := &recordingReporter{}
	ctx := withJobReporter(context.Background(), rep)
	ac := &AcquisitionContext{Track: TrackRef{ID: "t1"}, Selected: &ports.AudioCandidate{URL: "https://src/x"}}

	if err := RunPipeline(ctx, pipelineOf(), ac); err != nil {
		t.Fatal(err)
	}
	assertStepOrder(t, rep.stages, pipelineStageNames)
	if rep.srcURL != "https://src/x" {
		t.Errorf("source = %q, want the selected candidate URL", rep.srcURL)
	}
}

func TestRunPipeline_NoReporter_IsNoOp(t *testing.T) {
	ac := &AcquisitionContext{Track: TrackRef{ID: "t1"}}
	if err := RunPipeline(context.Background(), pipelineOf(), ac); err != nil {
		t.Fatal(err)
	}
}

func TestRunPipeline_TimesEveryStageThatRanAndOnlyThose(t *testing.T) {
	rep := &recordingReporter{}
	ctx := withJobReporter(context.Background(), rep)
	var log []string
	download := newMockStep("download", &log)
	download.executeErr = errors.New("no audio")

	_ = RunPipeline(ctx, pipelineOf(newMockStep("search", &log), newMockStep("select", &log), download), &AcquisitionContext{Track: TrackRef{ID: "t1"}})

	got := make([]string, 0, len(rep.timings))
	for stage := range rep.timings {
		got = append(got, stage)
	}
	slices.Sort(got)
	if want := []string{"download", "search", "select"}; !slices.Equal(got, want) {
		t.Errorf("timed stages = %v, want %v: the failed stage is timed, the skipped ones are absent", got, want)
	}
}

func TestSchedulerJobReporter_SumsRepeatedTimingsForAStage(t *testing.T) {
	log := newJobLog()
	log.register("t1", "")
	rep := schedulerJobReporter{ctx: context.Background(), log: log, trackID: "t1"}

	rep.timing("download.preview", 120*time.Millisecond)
	rep.timing("download.preview", 30*time.Millisecond)
	rep.fetched(ports.SourceFormat{ID: "18", Codec: "mp4a.40.2"})
	rep.stored(ports.StoredFile{Bytes: 4096, Codec: "mp3"})
	record := log.complete("t1", ports.JobSucceeded, "")

	if got := record.StageMs["download.preview"]; got != 150 {
		t.Errorf("download.preview = %dms, want 150 (summed across candidates)", got)
	}
	if record.Source.ID != "18" || record.Stored.Bytes != 4096 || record.Stored.Codec != "mp3" {
		t.Errorf("record source = %+v stored = %+v, want the reported format and file", record.Source, record.Stored)
	}
}

func TestSchedulerJobReporter_TheLastReportedSourceWins(t *testing.T) {
	log := newJobLog()
	log.register("t1", "")
	rep := schedulerJobReporter{ctx: context.Background(), log: log, trackID: "t1"}

	rep.source("https://music.youtube.com/watch?v=rank1")
	rep.source("https://www.youtube.com/watch?v=rank2")
	record := log.complete("t1", ports.JobSucceeded, "")

	if want := "https://www.youtube.com/watch?v=rank2"; record.ResolvedSource != want {
		t.Errorf("resolved source = %q, want %q (the candidate reported last)", record.ResolvedSource, want)
	}
}

type secondRankSource struct{ unknownTopicSource }

const (
	failingRank1URL = "https://www.youtube.com/watch?v=rank1AAAAAA"
	storedRank2URL  = "https://music.youtube.com/watch?v=rank2AAAAAA"
)

func (secondRankSource) Find(ctx context.Context, req ports.FindRequest) ([]ports.AudioCandidate, error) {
	found, err := unknownTopicSource{}.Find(ctx, req)
	first, second := found[0], found[0]
	first.URL, second.URL = failingRank1URL, storedRank2URL
	return []ports.AudioCandidate{first, second}, err
}

func (s secondRankSource) Fetch(ctx context.Context, c ports.AudioCandidate, outDir string) (ports.FetchedAudio, error) {
	if c.URL == failingRank1URL {
		return ports.FetchedAudio{}, errors.New("exit status 1")
	}
	return s.unknownTopicSource.Fetch(ctx, c, outDir)
}

func TestExecute_ReportsTheStoredCandidateAsTheSource(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	repo := newFakeTrackRepository()
	track, err := domain.NewTrack(userId, "Blinding Lights", "The Weeknd", "After Hours")
	if err != nil {
		t.Fatalf("new track: %v", err)
	}
	seconds := 200.0
	track.DurationSeconds = &seconds
	repo.tracks[track.ID.String()+":"+userId.String()] = track
	svc := newTestAcquireService(catalogbridge.NewTrackLedger(repo), NewSourceRegistry(secondRankSource{}),
		&bytesAudioStore{objects: map[string]string{}},
		WithRecordingResolver(&stubResolver{identity: ports.RecordingIdentity{MBID: "mb-blinding"}}),
		WithAudioIdentifier(&stubIdentifier{}))
	log := newJobLog()
	log.register(track.ID.String(), "")
	rep := schedulerJobReporter{ctx: context.Background(), log: log, events: events.NoopPublisher(), trackID: track.ID.String()}

	if err := svc.Execute(withJobReporter(context.Background(), rep), userId, track.ID); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	record := log.complete(track.ID.String(), ports.JobSucceeded, "")
	if record.ResolvedSource != storedRank2URL {
		t.Errorf("job source = %q, want the stored rank-2 candidate %q", record.ResolvedSource, storedRank2URL)
	}
}
