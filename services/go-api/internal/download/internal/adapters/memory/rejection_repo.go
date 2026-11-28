package memory

import (
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/ids"
	"context"
	"slices"
	"strings"
	"sync"
	"time"
)

type rejectionKey struct {
	track  string
	source string
}

type rejectionRow struct {
	basis      *domain.RejectionBasis
	rejectedAt time.Time
}

type RejectionRepo struct {
	mu        sync.Mutex
	now       clock.Func
	rows      map[rejectionKey]rejectionRow
	recorded  []domain.CandidateRejectionRecord
	since     time.Time
	activeErr error
	recordErr error
}

var _ ports.RejectionStore = (*RejectionRepo)(nil)

func NewRejectionRepo(now clock.Func) *RejectionRepo {
	return &RejectionRepo{now: now, rows: map[rejectionKey]rejectionRow{}}
}

func (r *RejectionRepo) FailActive(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.activeErr = err
}

func (r *RejectionRepo) FailRecord(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recordErr = err
}

func (r *RejectionRepo) Seed(trackID ids.TrackId, prior ...domain.PriorRejection) {
	r.mu.Lock()
	defer r.mu.Unlock()
	at := r.now()
	for _, p := range prior {
		r.rows[rejectionKey{track: trackID.String(), source: p.SourceKey}] = rejectionRow{basis: p.Basis, rejectedAt: at}
	}
}

func (r *RejectionRepo) Recorded() []domain.CandidateRejectionRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.recorded)
}

func (r *RejectionRepo) LastSince() time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.since
}

func (r *RejectionRepo) Record(ctx context.Context, recs []domain.CandidateRejectionRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.recordErr; err != nil {
		r.recorded = append(r.recorded, recs...)
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	at := r.now()
	for _, rec := range recs {
		basis := rec.Basis
		r.rows[rejectionKey{track: rec.TrackID, source: rec.SourceKey}] = rejectionRow{basis: &basis, rejectedAt: at}
	}
	r.recorded = append(r.recorded, recs...)
	return nil
}

func (r *RejectionRepo) Active(ctx context.Context, trackID ids.TrackId, since time.Time) ([]domain.PriorRejection, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.since = since
	if err := r.activeErr; err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var active []domain.PriorRejection
	for key, row := range r.rows {
		if key.track == trackID.String() && !row.rejectedAt.Before(since) {
			active = append(active, domain.PriorRejection{SourceKey: key.source, Basis: row.basis})
		}
	}
	slices.SortFunc(active, func(a, b domain.PriorRejection) int { return strings.Compare(a.SourceKey, b.SourceKey) })
	return active, nil
}
