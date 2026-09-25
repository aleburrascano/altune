package service

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/events"
	"context"
)

type jobReporterKey struct{}

type jobReporter interface {
	meta(title, artist, album string)
	stage(name string)
	source(url string)
	provenance(value string)
}

type noopJobReporter struct{}

func (noopJobReporter) meta(_, _, _ string) {}
func (noopJobReporter) stage(_ string)      {}
func (noopJobReporter) source(_ string)     {}
func (noopJobReporter) provenance(_ string) {}

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
	r.log.update(r.trackID, func(j *ports.JobRecord) { j.Stage = name })
	r.events.Publish(r.ctx, r.userId, events.TypeTrackAcquisitionProgress, map[string]any{
		"track_id": r.trackID,
		"stage":    name,
	})
}

func (r schedulerJobReporter) provenance(value string) {
	r.log.update(r.trackID, func(j *ports.JobRecord) { j.Provenance = value })
}

func (r schedulerJobReporter) source(url string) {
	r.log.update(r.trackID, func(j *ports.JobRecord) {
		if j.ResolvedSource == "" {
			j.ResolvedSource = url
		}
	})
}
