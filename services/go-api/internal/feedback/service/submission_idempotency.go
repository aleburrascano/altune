package service

import (
	"altune/go-api/internal/feedback/ports"
	"errors"
	"sync"
	"time"
)

// idempotencyTTL bounds how long a remembered submission stays dedup-eligible.
// A retry of a dropped 201, or a double-tapped Submit, arrives within seconds
// to minutes; past the window the key is forgotten and a fresh submission under
// it creates a new issue. It also bounds memory: settled entries older than the
// window are pruned on access.
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

func (s *submissionIdempotency) do(key string, create func() (ports.IssueRef, error)) (ports.IssueRef, error) {
	entry, mine := s.claim(key)
	if mine {
		ref, err := create()
		s.settle(key, entry, ref, err)
		return ref, err
	}
	<-entry.done
	if entry.outcome != nil {
		return entry.outcome.ref, entry.outcome.err
	}
	return s.do(key, create)
}

// claim returns the caller's own new entry (mine=true) or an existing one to
// wait on (mine=false), pruning expired entries first.
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

// prune drops settled entries whose TTL has passed. In-flight entries (awaiting
// settle) are kept regardless of age, so a slow create is never forgotten out
// from under its waiters.
func (s *submissionIdempotency) prune() {
	for key, entry := range s.entries {
		if entry.outcome != nil && s.now().Sub(entry.at) >= idempotencyTTL {
			delete(s.entries, key)
		}
	}
}
