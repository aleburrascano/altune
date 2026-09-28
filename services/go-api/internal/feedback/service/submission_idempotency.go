package service

import (
	"altune/go-api/internal/feedback/ports"
	"context"
	"errors"
	"sync"
	"time"
)

const idempotencyTTL = 30 * time.Minute

type issueOutcome struct {
	ref ports.IssueRef
	err error
}

type idempotencyEntry struct {
	done    chan struct{}
	outcome *issueOutcome
	at      time.Time
}

type submissionIdempotency struct {
	mu      sync.Mutex
	now     func() time.Time
	entries map[string]*idempotencyEntry
}

func newSubmissionIdempotency(now func() time.Time) *submissionIdempotency {
	return &submissionIdempotency{now: now, entries: make(map[string]*idempotencyEntry)}
}

var errCreateInterrupted = errors.New("idempotent create interrupted before it returned")

func (s *submissionIdempotency) do(
	ctx context.Context,
	key string,
	create func() (ports.IssueRef, error),
) (ports.IssueRef, error) {
	entry, mine := s.claim(key)
	if mine {
		return s.createAndSettle(key, entry, create)
	}
	if outcome := awaitOutcome(ctx, entry); outcome != nil {
		return outcome.ref, outcome.err
	}
	return s.do(ctx, key, create)
}

func (s *submissionIdempotency) createAndSettle(
	key string,
	entry *idempotencyEntry,
	create func() (ports.IssueRef, error),
) (ref ports.IssueRef, err error) {
	err = errCreateInterrupted
	defer func() { s.settle(key, entry, ref, err) }()
	return create()
}

func awaitOutcome(ctx context.Context, entry *idempotencyEntry) *issueOutcome {
	select {
	case <-entry.done:
		return entry.outcome
	case <-ctx.Done():
		return &issueOutcome{err: ctx.Err()}
	}
}

func (s *submissionIdempotency) claim(key string) (*idempotencyEntry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune()
	if existing, ok := s.entries[key]; ok {
		return existing, false
	}
	entry := &idempotencyEntry{done: make(chan struct{}), at: s.now()}
	s.entries[key] = entry
	return entry, true
}

func (s *submissionIdempotency) settle(key string, entry *idempotencyEntry, ref ports.IssueRef, err error) {
	s.mu.Lock()
	if isReplayable(err) {
		entry.outcome = &issueOutcome{ref: ref, err: err}
		entry.at = s.now()
	} else {
		delete(s.entries, key)
	}
	s.mu.Unlock()
	close(entry.done)
}

func isReplayable(err error) bool {
	return err == nil || mayHaveCreated(err)
}

func mayHaveCreated(err error) bool {
	var uncreated ports.TrackerUncreated
	return errors.As(err, &uncreated) && !uncreated.Uncreated()
}

func (s *submissionIdempotency) prune() {
	for key, entry := range s.entries {
		if entry.outcome != nil && s.now().Sub(entry.at) >= idempotencyTTL {
			delete(s.entries, key)
		}
	}
}
