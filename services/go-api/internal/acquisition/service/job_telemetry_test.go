package service

import (
	"altune/go-api/internal/acquisition/ports"
	"context"
	"errors"
	"slices"
	"testing"
	"time"
)

type recordingReporter struct {
	stages  []string
	srcURL  string
	prov    string
	timings map[string]time.Duration
	format  ports.SourceFormat
	file    ports.StoredFile
}

func (r *recordingReporter) meta(string, string, string)  {}
func (r *recordingReporter) stage(n string)               { r.stages = append(r.stages, n) }
func (r *recordingReporter) source(u string)              { r.srcURL = u }
func (r *recordingReporter) provenance(p string)          { r.prov = p }
func (r *recordingReporter) fetched(f ports.SourceFormat) { r.format = f }
func (r *recordingReporter) stored(f ports.StoredFile)    { r.file = f }
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
