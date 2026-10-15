package main

import (
	"altune/go-api/internal/shared/songfacts"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/google/uuid"
)

const reasonPinnedPrefix = "pinned: "

var (
	errMissingNote      = errors.New("note is required")
	errMissingEvidence  = errors.New("a replacement requires evidence")
	errReplacesNoAnchor = errors.New("replaces needs an isrc or an mb_recording_id")
	errMissingVersion   = errors.New("track_version is required")
	errBadStatus        = errors.New(`status must be "bound" or "unofficial"`)
	errBadISRC          = errors.New("isrc is not a canonical ISRC")
	errBadMBRecordingID = errors.New("mb_recording_id is not a UUID")
	errBoundNeedsAnchor = errors.New("a bound pin needs an isrc or an mb_recording_id")
	errDuplicateTrack   = errors.New("track_id appears more than once in the file")
	errPinsNotAList     = errors.New("pins file must hold a JSON list of pins")
	errPinsRejected     = errors.New("pins rejected")
)

type pin struct {
	TrackID       string
	TrackVersion  int
	Status        status
	ISRC          string
	MBRecordingID string
	DeezerTrackID string
	AppleSongID   string
	Note          string
	Evidence      json.RawMessage
	Replaces      *replaces
}

type replaces struct {
	ISRC          string
	MBRecordingID string
}

func (r replaces) matches(isrc, mbRecordingID string) bool {
	return (r.ISRC == "" || r.ISRC == isrc) && (r.MBRecordingID == "" || r.MBRecordingID == mbRecordingID)
}

type replacesFile struct {
	ISRC          string `json:"isrc"`
	MBRecordingID string `json:"mb_recording_id"`
}

func (f replacesFile) parsed() replaces {
	return replaces{ISRC: songfacts.CanonicalISRC(f.ISRC), MBRecordingID: strings.ToLower(strings.TrimSpace(f.MBRecordingID))}
}

type pinFile struct {
	TrackID       string          `json:"track_id"`
	TrackVersion  *int            `json:"track_version"`
	Status        string          `json:"status"`
	ISRC          string          `json:"isrc"`
	MBRecordingID string          `json:"mb_recording_id"`
	DeezerTrackID string          `json:"deezer_track_id"`
	AppleSongID   string          `json:"apple_song_id"`
	Note          string          `json:"note"`
	Evidence      json.RawMessage `json:"evidence"`
	Replaces      *replacesFile   `json:"replaces"`
}

type rejectedPin struct {
	Index   int
	TrackID string
	Err     error
}

func parsePins(raw []byte) ([]pin, []rejectedPin, error) {
	var entries []json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, nil, fmt.Errorf("%w: %w", errPinsNotAList, err)
	}
	var pins []pin
	var rejected []rejectedPin
	for index, entry := range entries {
		p, err := parsePin(entry)
		if err != nil {
			rejected = append(rejected, rejectedPin{Index: index, TrackID: p.TrackID, Err: err})
			continue
		}
		pins = append(pins, p)
	}
	pins, duplicates := withoutDuplicates(pins)
	return pins, append(rejected, duplicates...), nil
}

func parsePin(entry json.RawMessage) (pin, error) {
	var file pinFile
	decoder := json.NewDecoder(bytes.NewReader(entry))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&file); err != nil {
		return pin{}, fmt.Errorf("decode pin: %w", err)
	}
	p := pin{
		TrackID: strings.TrimSpace(file.TrackID), Status: status(file.Status), ISRC: songfacts.CanonicalISRC(file.ISRC),
		MBRecordingID: strings.ToLower(strings.TrimSpace(file.MBRecordingID)), DeezerTrackID: strings.TrimSpace(file.DeezerTrackID),
		AppleSongID: strings.TrimSpace(file.AppleSongID), Note: strings.TrimSpace(file.Note), Evidence: file.Evidence,
	}
	if file.Replaces != nil {
		replaced := file.Replaces.parsed()
		p.Replaces = &replaced
	}
	if file.TrackVersion != nil {
		p.TrackVersion = *file.TrackVersion
	}
	return p, p.validate(file.TrackVersion != nil)
}

func (p pin) validate(hasVersion bool) error {
	if _, err := uuid.Parse(p.TrackID); err != nil {
		return fmt.Errorf("track_id: %w", err)
	}
	switch {
	case !hasVersion:
		return errMissingVersion
	case p.TrackVersion < 0:
		return fmt.Errorf("track_version %d is negative", p.TrackVersion)
	case p.Status != statusBound && p.Status != statusUnofficial:
		return errBadStatus
	case p.Note == "":
		return errMissingNote
	case p.ISRC != "" && !songfacts.IsCanonicalISRC(p.ISRC):
		return errBadISRC
	case p.MBRecordingID != "" && !isUUID(p.MBRecordingID):
		return errBadMBRecordingID
	case p.Status == statusBound && p.ISRC == "" && p.MBRecordingID == "":
		return errBoundNeedsAnchor
	}
	return p.Replaces.validate(p.Evidence)
}

func (r *replaces) validate(proof json.RawMessage) error {
	switch {
	case r == nil:
		return nil
	case r.ISRC != "" && !songfacts.IsCanonicalISRC(r.ISRC):
		return fmt.Errorf("replaces: %w", errBadISRC)
	case r.MBRecordingID != "" && !isUUID(r.MBRecordingID):
		return fmt.Errorf("replaces: %w", errBadMBRecordingID)
	case r.ISRC == "" && r.MBRecordingID == "":
		return errReplacesNoAnchor
	case len(proof) == 0 || bytes.Equal(bytes.TrimSpace(proof), []byte("null")):
		return errMissingEvidence
	}
	return nil
}

func isUUID(value string) bool {
	_, err := uuid.Parse(value)
	return err == nil
}

func withoutDuplicates(pins []pin) ([]pin, []rejectedPin) {
	seen := map[string]int{}
	for _, p := range pins {
		seen[p.TrackID]++
	}
	var kept []pin
	var rejected []rejectedPin
	for index, p := range pins {
		if seen[p.TrackID] > 1 {
			rejected = append(rejected, rejectedPin{Index: index, TrackID: p.TrackID, Err: errDuplicateTrack})
			continue
		}
		kept = append(kept, p)
	}
	return kept, rejected
}

type pinTarget struct {
	Track                 libraryTrack
	IdentityStatus        status
	IdentityISRC          string
	IdentityMBRecordingID string
}

type pinSkip string

const (
	pinWrites               pinSkip = ""
	pinSkipUnknownTrack     pinSkip = "unknown-track"
	pinSkipTrackChanged     pinSkip = "track-changed"
	pinSkipAlreadyDecided   pinSkip = "already-identified"
	pinSkipNothingToReplace pinSkip = "nothing-to-replace"
	pinSkipReplaceMismatch  pinSkip = "replaces-mismatch"
	pinSkipLostRace         pinSkip = "lost-race"
)

type pinDecision struct {
	Pin   pin
	Track libraryTrack
	Skip  pinSkip
}

func decidePin(p pin, targets map[string]pinTarget) pinDecision {
	target, found := targets[p.TrackID]
	switch {
	case !found:
		return pinDecision{Pin: p, Skip: pinSkipUnknownTrack}
	case target.Track.Version != p.TrackVersion:
		return pinDecision{Pin: p, Track: target.Track, Skip: pinSkipTrackChanged}
	case p.Replaces != nil && target.IdentityStatus != statusBound:
		return pinDecision{Pin: p, Track: target.Track, Skip: pinSkipNothingToReplace}
	case p.Replaces != nil && !p.Replaces.matches(target.IdentityISRC, target.IdentityMBRecordingID):
		return pinDecision{Pin: p, Track: target.Track, Skip: pinSkipReplaceMismatch}
	case p.Replaces != nil:
		return pinDecision{Pin: p, Track: target.Track, Skip: pinWrites}
	case target.IdentityStatus != "" && target.IdentityStatus != statusUnbound:
		return pinDecision{Pin: p, Track: target.Track, Skip: pinSkipAlreadyDecided}
	}
	return pinDecision{Pin: p, Track: target.Track, Skip: pinWrites}
}

func (p pin) identity(t libraryTrack) identity {
	return identity{
		Status: p.Status, Reason: reasonPinnedPrefix + p.Note, ISRC: p.ISRC, MBRecordingID: p.MBRecordingID,
		DeezerTrackID: p.DeezerTrackID, AppleSongID: p.AppleSongID,
		Evidence: evidence{
			Title: t.Title, Artist: t.Artist, Album: t.Album, FileSeconds: t.FileSeconds,
			Sources: []sourceOutcome{}, Pin: p.Evidence,
		},
		Replaces: p.Replaces,
	}
}

type pinTargetLoader func(ctx context.Context, trackIDs []string) (map[string]pinTarget, error)

func runPins(ctx context.Context, raw []byte, loadTargets pinTargetLoader, sink identitySink, runID uuid.UUID, out io.Writer) error {
	pins, rejected, err := parsePins(raw)
	if err != nil {
		return err
	}
	decisions, err := decidePins(ctx, pins, loadTargets)
	if err != nil {
		return err
	}
	saveErr := savePins(ctx, decisions, sink, runID)
	writePinReport(out, decisions, rejected, sink != nil, runID)
	if saveErr != nil {
		return saveErr
	}
	if len(rejected) > 0 {
		return fmt.Errorf("%w: %d of %d", errPinsRejected, len(rejected), len(rejected)+len(pins))
	}
	return nil
}

func decidePins(ctx context.Context, pins []pin, loadTargets pinTargetLoader) ([]pinDecision, error) {
	ids := make([]string, 0, len(pins))
	for _, p := range pins {
		ids = append(ids, p.TrackID)
	}
	targets, err := loadTargets(ctx, ids)
	if err != nil {
		return nil, err
	}
	decisions := make([]pinDecision, 0, len(pins))
	for _, p := range pins {
		decisions = append(decisions, decidePin(p, targets))
	}
	return decisions, nil
}

func savePins(ctx context.Context, decisions []pinDecision, sink identitySink, runID uuid.UUID) error {
	if sink == nil {
		return nil
	}
	for i, d := range decisions {
		if d.Skip != pinWrites {
			continue
		}
		verdict, err := sink.save(ctx, runID, d.Track, d.Pin.identity(d.Track))
		if err != nil {
			return fmt.Errorf("aborted pinning %s: %w", d.Pin.TrackID, err)
		}
		if verdict == saveSkipped {
			decisions[i].Skip = pinSkipLostRace
		}
	}
	return nil
}

func writePinReport(out io.Writer, decisions []pinDecision, rejected []rejectedPin, apply bool, runID uuid.UUID) {
	mode := "dry run, nothing written"
	if apply {
		mode = "applied"
	}
	writes := 0
	_, _ = fmt.Fprintf(out, "pins run %s (%s): %d pins, %d rejected\n", runID, mode, len(decisions)+len(rejected), len(rejected))
	for _, r := range rejected {
		_, _ = fmt.Fprintf(out, "  rejected   pin %d %s: %v\n", r.Index, r.TrackID, r.Err)
	}
	for _, d := range decisions {
		_, _ = fmt.Fprintln(out, pinLine(d))
		if d.Skip == pinWrites {
			writes++
		}
	}
	_, _ = fmt.Fprintf(out, "pinned: %d, skipped: %d, rejected: %d\n", writes, len(decisions)-writes, len(rejected))
}

func pinLine(d pinDecision) string {
	if d.Skip != pinWrites {
		return fmt.Sprintf("  skipped    %s (%s): %s", d.Pin.TrackID, d.Skip, pinSkipDetail(d))
	}
	return fmt.Sprintf("  %-10s %s - %s (%s): %s%s", d.Pin.Status, d.Track.Artist, d.Track.Title, d.Pin.TrackID, reasonPinnedPrefix+d.Pin.Note, replacementSuffix(d.Pin.Replaces))
}

func replacementSuffix(r *replaces) string {
	if r == nil {
		return ""
	}
	return fmt.Sprintf(" [replaces isrc=%q mb_recording_id=%q]", r.ISRC, r.MBRecordingID)
}

func pinSkipDetail(d pinDecision) string {
	switch d.Skip {
	case pinSkipUnknownTrack:
		return "no such track"
	case pinSkipTrackChanged:
		return fmt.Sprintf("pinned against version %d, track is now %d", d.Pin.TrackVersion, d.Track.Version)
	case pinSkipNothingToReplace:
		return "replaces names an identity, but the track has no bound row"
	case pinSkipReplaceMismatch:
		return "the stored isrc or mb_recording_id differs from replaces"
	case pinSkipLostRace:
		return "the track changed or was identified while the run was writing"
	}
	return "the track already has an identity row that is not unbound"
}
