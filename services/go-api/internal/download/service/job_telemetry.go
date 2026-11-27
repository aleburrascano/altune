package service

import (
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/telemetry"
	"context"
	"time"
)

func startStage(ctx context.Context, ac *AcquisitionContext, now clock.Func, stage string) (context.Context, func(error)) {
	stageCtx, span := telemetry.StartStage(ctx, now, stage)
	return stageCtx, func(err error) {
		ac.Job.Progress().Timing(stage, span.End(err))
	}
}

type schedulerJobReporter struct {
	log     *jobLog
	events  events.Publisher
	trackID ids.TrackId
	userId  ids.UserId
}

func (r schedulerJobReporter) Meta(title, artist, album string) {
	r.log.update(r.trackID, func(j *ports.JobRecord) { j.Title, j.Artist, j.Album = title, artist, album })
}

func (r schedulerJobReporter) Stage(ctx context.Context, name string) {
	var title, artist string
	r.log.update(r.trackID, func(j *ports.JobRecord) {
		j.Stage = name
		title, artist = j.Title, j.Artist
	})
	r.events.Publish(ctx, r.userId, events.TrackAcquisitionProgress{
		TrackID: r.trackID.String(),
		Stage:   name,
		Title:   title,
		Artist:  artist,
	})
}

func (r schedulerJobReporter) Provenance(value string) {
	r.log.update(r.trackID, func(j *ports.JobRecord) { j.Provenance = value })
}

func (r schedulerJobReporter) Source(url string) {
	r.log.update(r.trackID, func(j *ports.JobRecord) { j.ResolvedSource = url })
}

func (r schedulerJobReporter) Timing(stage string, d time.Duration) {
	r.log.update(r.trackID, func(j *ports.JobRecord) {
		if j.StageMs == nil {
			j.StageMs = map[string]int64{}
		}
		j.StageMs[stage] += d.Milliseconds()
	})
}

func (r schedulerJobReporter) Fetched(format ports.SourceFormat) {
	r.log.update(r.trackID, func(j *ports.JobRecord) { j.Source = format })
}

func (r schedulerJobReporter) Stored(file ports.StoredFile) {
	r.log.update(r.trackID, func(j *ports.JobRecord) { j.Stored = file })
}

func (r schedulerJobReporter) Decided(confidence float64, sourceKey string, target, measured float64) {
	r.log.update(r.trackID, func(j *ports.JobRecord) {
		j.Confidence, j.SourceKey, j.TargetSeconds, j.MeasuredSeconds = confidence, sourceKey, target, measured
	})
}
