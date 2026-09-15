package catalogtest

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
	"context"
	"sort"
)

// OrphanedAudioQueue is an in-memory ports.OrphanedAudioQueue. AudioUsage reads
// the tracks of Tracks, so a test can prove the sweep's reference gate against
// the same fake repository the delete path wrote to.
type OrphanedAudioQueue struct {
	Tracks  *TrackRepo
	Orphans map[string]*ports.OrphanedAudio
	Errors  map[string]string // last recorded attempt cause per key

	// ErrOnRecord, ErrOnList and ErrOnUsage fail the matching call.
	ErrOnRecord error
	ErrOnList   error
	ErrOnUsage  error
}

var _ ports.OrphanedAudioQueue = (*OrphanedAudioQueue)(nil)

func NewOrphanedAudioQueue(tracks *TrackRepo) *OrphanedAudioQueue {
	return &OrphanedAudioQueue{Tracks: tracks, Orphans: map[string]*ports.OrphanedAudio{}, Errors: map[string]string{}}
}

func (q *OrphanedAudioQueue) RecordOrphanedAudio(_ context.Context, orphan ports.OrphanedAudio) error {
	if q.ErrOnRecord != nil {
		return q.ErrOnRecord
	}
	orphan.Attempts = 0
	q.Orphans[orphan.AudioRef] = &orphan
	return nil
}

func (q *OrphanedAudioQueue) ListOrphanedAudio(_ context.Context, limit int) ([]ports.OrphanedAudio, error) {
	if q.ErrOnList != nil {
		return nil, q.ErrOnList
	}
	out := make([]ports.OrphanedAudio, 0, len(q.Orphans))
	for _, o := range q.Orphans {
		out = append(out, *o)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].AudioRef < out[j].AudioRef })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (q *OrphanedAudioQueue) AudioUsage(_ context.Context, audioRef string, owner shared.UserId) (ports.AudioUsage, error) {
	if q.ErrOnUsage != nil {
		return ports.AudioReferenced, q.ErrOnUsage
	}
	acquiring := false
	for _, t := range q.Tracks.Tracks {
		if t == nil {
			continue
		}
		if t.AudioRef != nil && *t.AudioRef == audioRef {
			return ports.AudioReferenced, nil
		}
		if t.UserId == owner && t.AcquisitionStatus == domain.AcquisitionPending {
			acquiring = true
		}
	}
	if acquiring {
		return ports.AudioOwnerAcquiring, nil
	}
	return ports.AudioUnused, nil
}

func (q *OrphanedAudioQueue) ResolveOrphanedAudio(_ context.Context, audioRef string) error {
	delete(q.Orphans, audioRef)
	return nil
}

func (q *OrphanedAudioQueue) MarkOrphanedAudioAttempt(_ context.Context, audioRef, cause string) error {
	if o, ok := q.Orphans[audioRef]; ok {
		o.Attempts++
		q.Errors[audioRef] = cause
	}
	return nil
}
