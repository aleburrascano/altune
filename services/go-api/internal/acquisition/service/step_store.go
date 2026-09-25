package service

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared/textnorm"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"path"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

// rollbackDeleteTries bounds how many times a rollback re-attempts the
// compensating delete before surfacing the failure to the caller.
const rollbackDeleteTries = 3

type StoreStep struct {
	audioStore ports.AudioWriter
	prober     ports.AudioProber
	// trackRefs guards the compensating delete against an object another track
	// still serves; ownTrackID is the track this attempt is acquiring, the one
	// reference that does not count. Both stay unset where there is no track
	// store to ask (the eval harness, the reacquire command), which leaves the
	// delete as it was.
	trackRefs  ports.AudioRefLookup
	ownTrackID domain.TrackId
	attemptID  func() string
	// deleteTries and sleep make the compensating delete a bounded, retryable
	// operation; sleep is a seam so tests need not wait on real backoff.
	deleteTries int
	sleep       func(time.Duration)
}

func NewStoreStep(audioStore ports.AudioWriter, opts ...func(*StoreStep)) *StoreStep {
	s := &StoreStep{
		audioStore:  audioStore,
		attemptID:   uuid.NewString,
		deleteTries: rollbackDeleteTries,
		sleep:       time.Sleep,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func WithStoreProber(p ports.AudioProber) func(*StoreStep) {
	return func(s *StoreStep) { s.prober = p }
}

func WithStoreAudioRefGuard(refs ports.AudioRefLookup, ownTrackID domain.TrackId) func(*StoreStep) {
	return func(s *StoreStep) {
		s.trackRefs = refs
		s.ownTrackID = ownTrackID
	}
}

func (s *StoreStep) Name() string { return stepNameStore }

func (s *StoreStep) Execute(ctx context.Context, ac *AcquisitionContext, _ afterTag) (afterStore, error) {
	if ac.TempPath == "" {
		return afterStore{}, fmt.Errorf("no temp file to store")
	}

	if s.prober != nil {
		if err := s.prober.ValidateDecodable(ctx, ac.TempPath); err != nil {
			return afterStore{}, withCancellation(ctx, fmt.Errorf("final audio failed decode validation: %w", err))
		}
	}

	audioRef := BuildAudioRef(ac.Track, ac.TempPath)
	if ac.Replace.PreservedRef != "" {
		// A replace must never write over the object the track is still
		// serving: the canonical key commonly equals PreservedRef, and only
		// update_track confirms the swap. Stage the new audio under its own
		// key; the track points at it once update_track commits, and
		// ExecuteReplace deletes the superseded object only after that.
		audioRef = stagedReplaceRef(audioRef, s.attemptID())
	}
	ac.AudioRef = audioRef

	if err := s.audioStore.Store(ctx, ac.TempPath, audioRef); err != nil {
		return afterStore{}, withCancellation(ctx, fmt.Errorf("store audio: %w", err))
	}

	return afterStore{}, nil
}

func (s *StoreStep) Rollback(ctx context.Context, ac *AcquisitionContext) error {
	if ac.AudioRef == "" || s.stillServesATrack(ctx, ac) {
		return nil
	}
	return s.deleteWithRetry(ctx, ac.AudioRef)
}

// stillServesATrack reports whether the stored object is another track's
// audio: the replaced track's own preserved ref, or — since the canonical ref
// is shared by tracks with equivalent metadata — some other track's row.
func (s *StoreStep) stillServesATrack(ctx context.Context, ac *AcquisitionContext) bool {
	if ac.AudioRef == ac.Replace.PreservedRef {
		slog.WarnContext(ctx, "acquisition.rollback_kept_preserved_audio", "audio_ref", ac.AudioRef)
		return true
	}
	if !s.sharedWithAnotherTrack(ctx, ac.AudioRef) {
		return false
	}
	slog.WarnContext(ctx, "acquisition.rollback_kept_shared_audio",
		"audio_ref", ac.AudioRef, "track_id", ac.Track.ID)
	return true
}

// sharedWithAnotherTrack reads an unanswerable check as "shared": an object
// kept is an orphan the reconcile sweep reaps, an object wrongly deleted is a
// Ready track whose file is gone.
func (s *StoreStep) sharedWithAnotherTrack(ctx context.Context, audioRef string) bool {
	if s.trackRefs == nil {
		return false
	}
	inUse, err := s.trackRefs.AudioRefInUse(ctx, audioRef, s.ownTrackID)
	if err != nil {
		slog.ErrorContext(ctx, "acquisition.rollback_audio_usage_unknown",
			"audio_ref", audioRef, "error", logSafeError(err))
		return true
	}
	return inUse
}

// deleteWithRetry makes the compensating delete retryable: transient failures
// are re-attempted with backoff, and an exhausted or cancelled retry surfaces a
// wrapped error so the caller can reap the orphan instead of losing it silently.
func (s *StoreStep) deleteWithRetry(ctx context.Context, audioRef string) error {
	var err error
	for attempt := 1; attempt <= s.deleteTries; attempt++ {
		if err = s.audioStore.Delete(ctx, audioRef); err == nil {
			return nil
		}
		slog.WarnContext(ctx, "acquisition.rollback_delete_retrying",
			"audio_ref", audioRef, "attempt", attempt, "error", err)
		if ctx.Err() != nil {
			break
		}
		if attempt < s.deleteTries {
			s.sleep(time.Duration(attempt) * 100 * time.Millisecond)
		}
	}
	slog.ErrorContext(ctx, "orphaned audio file after rollback",
		"audio_ref", audioRef, "error", err)
	return fmt.Errorf("rollback delete audio %q: %w", audioRef, err)
}

// stagedReplaceRef derives a replace attempt's own key from the canonical ref,
// keeping the extension last because it drives the served content type.
func stagedReplaceRef(canonical, attemptID string) string {
	ext := path.Ext(canonical)
	return strings.TrimSuffix(canonical, ext) + ".replace-" + attemptID + ext
}

// BuildAudioRef builds the canonical storage path for a freshly acquired track.
// Metadata segments are canonicalized (NFKC, case-fold, diacritic-strip) so the
// same artist in different case/Unicode form resolves to one physical folder.
func BuildAudioRef(track TrackRef, tempPath string) string {
	return buildAudioRef(track, tempPath, normalizePathComponent)
}

// BuildLegacyAudioRef reproduces the pre-normalization layout (case-preserving,
// reserved-char-stripped only). cmd/backfillaudio uses it to locate objects that
// were stored before BuildAudioRef began normalizing; migrating those objects to
// the canonical layout is a separate data-migration task.
func BuildLegacyAudioRef(track TrackRef, tempPath string) string {
	return buildAudioRef(track, tempPath, sanitizePathComponent)
}

func BuildLegacyAudioRefUncapped(track TrackRef, tempPath string) string {
	return buildAudioRef(track, tempPath, sanitizePathComponentUncapped)
}

func buildAudioRef(track TrackRef, tempPath string, segment func(string) string) string {
	artist := segment(track.Artist)
	album := track.Album
	if album == "" {
		album = "Unknown Album"
	}
	album = segment(album)
	title := segment(track.Title)

	ext := filepath.Ext(tempPath)
	if ext == "" {
		ext = ".mp3"
	}
	return strings.Join([]string{track.UserID, artist, album, title + ext}, "/")
}

// normalizePathComponent canonicalizes a metadata field into a stable path
// segment. It applies the same normalization used for matching (NFKC,
// case-fold, diacritic-strip) so that textually-equivalent-but-differently-
// cased/composed values map to one physical folder, then strips any
// filesystem-reserved characters that remain.
func normalizePathComponent(s string) string {
	return sanitizePathComponent(textnorm.NormalizeForMatch(s))
}

func sanitizePathComponent(s string) string {
	return capSegmentBytes(sanitizePathComponentUncapped(s))
}

func sanitizePathComponentUncapped(s string) string {
	if s == "" {
		return "Unknown"
	}
	forbidden := `<>:"/\|?*;`
	var b strings.Builder
	for _, r := range s {
		if !strings.ContainsRune(forbidden, r) && (unicode.IsSpace(r) || !unicode.IsControl(r)) {
			b.WriteRune(r)
		}
	}
	result := strings.Join(strings.Fields(b.String()), " ")
	if result == "" {
		return "Unknown"
	}
	if strings.Trim(result, ".") == "" {
		return "Unknown"
	}
	return result
}

const (
	maxSegmentBytes = 120
	segmentHashLen  = 8
)

func capSegmentBytes(s string) string {
	if len(s) <= maxSegmentBytes {
		return s
	}
	sum := sha256.Sum256([]byte(s))
	suffix := "-" + hex.EncodeToString(sum[:])[:segmentHashLen]
	cut := maxSegmentBytes - len(suffix)
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + suffix
}
