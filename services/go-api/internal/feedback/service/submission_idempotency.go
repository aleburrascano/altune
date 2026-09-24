package service

import (
	"altune/go-api/internal/feedback/ports"
	"sync"
	"time"
)

// idempotencyTTL bounds how long a remembered submission stays dedup-eligible.
// A retry of a dropped 201, or a double-tapped Submit, arrives within seconds
// to minutes; past the window the key is forgotten and a fresh submission under
// it creates a new issue. It also bounds memory: settled entries older than the
// window are pruned on access.
const idempotencyTTL = 30 * time.Minute

// idempotencyEntry is one remembered submission. done is closed once the first
// caller under the key finishes; ok/ref hold a successfully created issue so a
// later caller replays it instead of creating a second issue.
type idempotencyEntry struct {
	done chan struct{}
	ref  ports.IssueRef
	ok   bool
	at   time.Time
}

// submissionIdempotency dedups submissions by a caller-supplied key: the first
// call under a key runs the create, concurrent duplicates wait for and replay
// its result, and a sequential retry within the TTL replays the cached issue. A
// failed create is never remembered, so a genuine failure stays retryable.
type submissionIdempotency struct {
	mu      sync.Mutex
	now     func() time.Time
	entries map[string]*idempotencyEntry
}

func newSubmissionIdempotency(now func() time.Time) *submissionIdempotency {
	return &submissionIdempotency{now: now, entries: make(map[string]*idempotencyEntry)}
}

// do runs create at most once per live key: the winning caller executes it and
// records a success, while every other caller sharing the key replays that
// result without touching create. A failed attempt is retried fresh.
func (s *submissionIdempotency) do(key string, create func() (ports.IssueRef, error)) (ports.IssueRef, error) {
	entry, mine := s.claim(key)
	if mine {
		ref, err := create()
		s.settle(key, entry, ref, err)
		return ref, err
	}
	<-entry.done
	if entry.ok {
		return entry.ref, nil
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

// settle records a successful result for replay, or forgets the key on failure
// so the submission stays retryable, then wakes every waiter.
func (s *submissionIdempotency) settle(key string, entry *idempotencyEntry, ref ports.IssueRef, err error) {
	s.mu.Lock()
	if err != nil {
		delete(s.entries, key)
	} else {
		entry.ref = ref
		entry.ok = true
		entry.at = s.now()
	}
	s.mu.Unlock()
	close(entry.done)
}

// prune drops settled entries whose TTL has passed. In-flight entries (awaiting
// settle) are kept regardless of age, so a slow create is never forgotten out
// from under its waiters.
func (s *submissionIdempotency) prune() {
	for key, entry := range s.entries {
		if entry.ok && s.now().Sub(entry.at) >= idempotencyTTL {
			delete(s.entries, key)
		}
	}
}
