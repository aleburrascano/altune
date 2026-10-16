package service

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/events"
	"context"
	"time"
)

type jobReporterKey struct{}

type jobReporter interface {
	meta(title, artist, album string)
	stage(name string)
	source(url string)
	provenance(value string)
	timing(stage string, d time.Duration)
	fetched(format ports.SourceFormat)
	stored(file ports.StoredFile)
}

type noopJobReporter struct{}

func (noopJobReporter) meta(_, _, _ string)              {}
func (noopJobReporter) stage(_ string)                   {}
func (noopJobReporter) source(_ string)                  {}
func (noopJobReporter) provenance(_ string)              {}
func (noopJobReporter) timing(_ string, _ time.Duration) {}
func (noopJobReporter) fetched(_ ports.SourceFormat)     {}
func (noopJobReporter) stored(_ ports.StoredFile)        {}

func timeStage(ctx context.Context, stage string, start time.Time) {
	jobReporterFrom(ctx).timing(stage, time.Since(start))
}

func withJobReporter(ctx context.Context, r jobReporter) context.Context {
	return context.WithValue(ctx, jobReporterKey{}, r)
}

func jobReporterFrom(ctx context.Context) jobReporter {
	if r, ok := ctx.Value(jobReporterKey{}).(jobReporter); ok && r != nil {
		return r
	}
	return noopJobReporter{}
}

type schedulerJobReporter struct {
	ctx     context.Context
	log     *jobLog
	events  events.Publisher
	trackID string
	userId  shared.UserId
}

func (r schedulerJobReporter) meta(title, artist, album string) {
	r.log.update(r.trackID, func(j *ports.JobRecord) { j.Title, j.Artist, j.Album = title, artist, album })
}

func (r schedulerJobReporter) stage(name string) {
	var title, artist string
	r.log.update(r.trackID, func(j *ports.JobRecord) {
		j.Stage = name
		title, artist = j.Title, j.Artist
	})
	r.events.Publish(r.ctx, r.userId, events.TypeTrackAcquisitionProgress, map[string]any{
		"track_id": r.trackID,
		"stage":    name,
		"title":    title,
		"artist":   artist,
	})
}

func (r schedulerJobReporter) provenance(value string) {
	r.log.update(r.trackID, func(j *ports.JobRecord) { j.Provenance = value })
}

func (r schedulerJobReporter) source(url string) {
	r.log.update(r.trackID, func(j *ports.JobRecord) { j.ResolvedSource = url })
}

func (r schedulerJobReporter) timing(stage string, d time.Duration) {
	r.log.update(r.trackID, func(j *ports.JobRecord) {
		if j.StageMs == nil {
			j.StageMs = map[string]int64{}
		}
		j.StageMs[stage] += d.Milliseconds()
	})
}

func (r schedulerJobReporter) fetched(format ports.SourceFormat) {
	r.log.update(r.trackID, func(j *ports.JobRecord) { j.Source = format })
}

func (r schedulerJobReporter) stored(file ports.StoredFile) {
	r.log.update(r.trackID, func(j *ports.JobRecord) { j.Stored = file })
}
