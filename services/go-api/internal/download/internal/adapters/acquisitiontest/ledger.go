package acquisitiontest

import (
	acqdomain "altune/go-api/internal/download/internal/core/domain"
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/shared/downloadprovenance"
	"altune/go-api/internal/shared/ids"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
)

var ErrVersionConflict = errors.New("track version conflict")

const (
	maxRejectedSourceKeys = 25
)

type LedgerTrack struct {
	ports.TrackSnapshot
	ClaimNumber   ports.ClaimNumber
	FailureReason string
	Provenance    downloadprovenance.AcquisitionProvenance
	Confidence    float64
}

type Ledger struct {
	mu       sync.Mutex
	tracks   map[ids.TrackId]LedgerTrack
	writeErr error
}

var _ ports.LibraryTracks = (*Ledger)(nil)

func NewLedger(tracks ...LedgerTrack) *Ledger {
	l := &Ledger{tracks: make(map[ids.TrackId]LedgerTrack)}
	for _, track := range tracks {
		l.Seed(track)
	}
	return l
}

func (l *Ledger) Seed(track LedgerTrack) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.tracks[track.ID] = copyTrack(track)
}

func (l *Ledger) Track(id ids.TrackId) (LedgerTrack, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	track, ok := l.tracks[id]
	if !ok {
		return LedgerTrack{}, false
	}
	return withStreamable(copyTrack(track)), true
}

func (l *Ledger) FailWrites(err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.writeErr = err
}

func (l *Ledger) Get(_ context.Context, id ids.TrackId, userId ids.UserId) (*ports.TrackSnapshot, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	track, ok := l.tracks[id]
	if !ok || track.UserId != userId {
		return nil, nil
	}
	snapshot := withStreamable(copyTrack(track)).TrackSnapshot
	return &snapshot, nil
}

func (l *Ledger) AudioRefInUse(_ context.Context, audioRef string, excludeTrackID ids.TrackId) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for id, track := range l.tracks {
		if id != excludeTrackID && track.AudioRef != nil && *track.AudioRef == audioRef {
			return true, nil
		}
	}
	return false, nil
}

func (l *Ledger) Complete(_ context.Context, id ids.TrackId, userId ids.UserId, fence ports.ClaimNumber, c ports.Completion) error {
	return l.update(id, userId, fence, func(track *LedgerTrack) error {
		if err := settleAudio(track, c); err != nil {
			return err
		}
		recordCompletion(track, c)
		if c.Relabel != nil {
			if err := relabel(track, *c.Relabel); err != nil {
				return err
			}
		}
		adoptISRC(track, c.ISRC)
		return nil
	})
}

func (l *Ledger) Fail(_ context.Context, id ids.TrackId, userId ids.UserId, fence ports.ClaimNumber, reason string) error {
	return l.update(id, userId, fence, func(track *LedgerTrack) error {
		if track.Status != ports.TrackPending {
			return illegalTransition("fail acquisition", track.Status)
		}
		if reason == "" {
			return errors.New("failure reason required for failed status")
		}
		track.Status, track.FailureReason, track.AudioRef = ports.TrackFailed, reason, nil
		return nil
	})
}

func (l *Ledger) RevertAt(_ context.Context, snapshot ports.TrackSnapshot) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	track, ok := l.tracks[snapshot.ID]
	if !ok || track.UserId != snapshot.UserId {
		return ports.ErrTrackNotFound
	}
	if track.Version != snapshot.Version {
		return fmt.Errorf("revert to pending at version %d, track is at %d: %w", snapshot.Version, track.Version, ErrVersionConflict)
	}
	if track.Status == ports.TrackPending {
		return illegalTransition("revert to pending", track.Status)
	}
	track.Status, track.AudioRef, track.FailureReason = ports.TrackPending, nil, ""
	return l.write(track)
}

func (l *Ledger) update(id ids.TrackId, userId ids.UserId, fence ports.ClaimNumber, mutate func(*LedgerTrack) error) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	stored, ok := l.tracks[id]
	if !ok || stored.UserId != userId {
		return ports.ErrTrackNotFound
	}
	if fence > 0 && stored.ClaimNumber > 0 && fence != stored.ClaimNumber {
		return fmt.Errorf("track %s claimed by fence %d, caller holds %d: %w", id, stored.ClaimNumber, fence, acqdomain.ErrLeaseLost)
	}
	track := copyTrack(stored)
	if err := mutate(&track); err != nil {
		return err
	}
	return l.write(track)
}

func (l *Ledger) write(track LedgerTrack) error {
	if l.writeErr != nil {
		return fmt.Errorf("update track: %w", l.writeErr)
	}
	track.Version++
	l.tracks[track.ID] = track
	return nil
}

func settleAudio(track *LedgerTrack, c ports.Completion) error {
	if c.AudioRef == "" {
		return errors.New("audio_ref required for ready status")
	}
	streamable := track.Status == ports.TrackReady && track.AudioRef != nil
	if c.ReplaceAudio && !streamable {
		return illegalTransition("replace audio", track.Status)
	}
	if !c.ReplaceAudio && streamable && *track.AudioRef != c.AudioRef {
		return illegalTransition("mark ready", track.Status)
	}
	ref := c.AudioRef
	track.Status, track.AudioRef, track.FailureReason = ports.TrackReady, &ref, ""
	return nil
}

func recordCompletion(track *LedgerTrack, c ports.Completion) {
	if c.Duration.Known() {
		duration := c.Duration.Seconds()
		track.StoredDurationSeconds = &duration
	}
	if c.Provenance.Valid() {
		track.Provenance = c.Provenance
	}
	track.RejectedSourceKeys = withRejected(track.RejectedSourceKeys, c.RejectedSourceKeys)
	if c.SourceURL != "" {
		url := c.SourceURL
		track.AudioSourceURL = &url
	}
	if c.Evidence != nil && validConfidence(c.Confidence, c.Evidence) {
		track.Confidence, track.Evidence = c.Confidence, slices.Clone(c.Evidence)
	}
}

func withRejected(stored, rejected []string) []string {
	for _, key := range rejected {
		if key != "" && !slices.Contains(stored, key) {
			stored = append(stored, key)
			stored = stored[max(0, len(stored)-maxRejectedSourceKeys):]
		}
	}
	return stored
}

func validConfidence(confidence float64, evidence json.RawMessage) bool {
	inRange := confidence >= 0 && confidence <= 1
	return inRange && (len(evidence) == 0 || json.Valid(evidence))
}

func adoptISRC(track *LedgerTrack, isrc string) {
	if isrc != "" && (track.ISRC == nil || *track.ISRC == "") {
		track.ISRC = &isrc
	}
}

func relabel(track *LedgerTrack, label ports.TrackLabel) error {
	title := strings.TrimSpace(label.Title)
	if title == "" || label.MBID == "" {
		return fmt.Errorf("relabel as recording %q titled %q: an MBID and a title are required", label.MBID, label.Title)
	}
	album := strings.TrimSpace(label.Album)
	if album == "" {
		album = title
	}
	mbid := label.MBID
	track.Title, track.Album, track.CatalogMBID = title, album, &mbid
	track.Year, track.ISRC, track.CatalogDurationSeconds = labelOptionals(label)
	return nil
}

func labelOptionals(label ports.TrackLabel) (year *int, isrc *string, seconds *float64) {
	if label.Year != 0 {
		year = &label.Year
	}
	if label.ISRC != "" {
		isrc = &label.ISRC
	}
	if label.DurationSeconds > 0 {
		seconds = &label.DurationSeconds
	}
	return year, isrc, seconds
}

type illegalTransitionError struct {
	op     string
	status ports.TrackStatus
}

func illegalTransition(op string, status ports.TrackStatus) error {
	return illegalTransitionError{op: op, status: status}
}

func (e illegalTransitionError) Error() string {
	return fmt.Sprintf("%s from status %d: %v", e.op, e.status, ports.ErrIllegalTransition)
}

func (e illegalTransitionError) Unwrap() error { return ports.ErrIllegalTransition }

func copyTrack(track LedgerTrack) LedgerTrack {
	track.CatalogSources = slices.Clone(track.CatalogSources)
	track.RejectedSourceKeys = slices.Clone(track.RejectedSourceKeys)
	track.Evidence = slices.Clone(track.Evidence)
	track.CatalogDurationSeconds = clonePointer(track.CatalogDurationSeconds)
	track.StoredDurationSeconds = clonePointer(track.StoredDurationSeconds)
	track.CatalogMBID = clonePointer(track.CatalogMBID)
	track.CatalogExplicit = clonePointer(track.CatalogExplicit)
	track.ISRC = clonePointer(track.ISRC)
	track.Year = clonePointer(track.Year)
	track.TrackNumber = clonePointer(track.TrackNumber)
	track.AlbumArtist = clonePointer(track.AlbumArtist)
	track.Genre = clonePointer(track.Genre)
	track.AudioRef = clonePointer(track.AudioRef)
	track.AudioSourceURL = clonePointer(track.AudioSourceURL)
	return track
}

func clonePointer[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func withStreamable(track LedgerTrack) LedgerTrack {
	track.Streamable = track.Status == ports.TrackReady && track.AudioRef != nil
	return track
}
