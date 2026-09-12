package service

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/shared/textnorm"
)

type StoreStep struct {
	audioStore ports.AudioWriter
	prober     ports.AudioProber
}

func NewStoreStep(audioStore ports.AudioWriter, opts ...func(*StoreStep)) *StoreStep {
	s := &StoreStep{audioStore: audioStore}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func WithStoreProber(p ports.AudioProber) func(*StoreStep) {
	return func(s *StoreStep) { s.prober = p }
}

func (s *StoreStep) Name() string { return "store" }

func (s *StoreStep) Execute(ctx context.Context, ac *AcquisitionContext) error {
	if ac.TempPath == "" {
		return fmt.Errorf("no temp file to store")
	}

	if s.prober != nil {
		if err := s.prober.ValidateDecodable(ctx, ac.TempPath); err != nil {
			return fmt.Errorf("final audio failed decode validation: %w", err)
		}
	}

	audioRef := BuildAudioRef(ac.Track, ac.TempPath)
	ac.AudioRef = audioRef

	if err := s.audioStore.Store(ctx, ac.TempPath, audioRef); err != nil {
		return fmt.Errorf("store audio: %w", err)
	}

	return nil
}

func (s *StoreStep) Rollback(ctx context.Context, ac *AcquisitionContext) error {
	if ac.AudioRef == "" {
		return nil
	}
	if ac.AudioRef == ac.Replace.PreservedRef {
		slog.WarnContext(ctx, "acquisition.rollback_kept_preserved_audio", "audio_ref", ac.AudioRef)
		return nil
	}
	if err := s.audioStore.Delete(ctx, ac.AudioRef); err != nil {
		slog.ErrorContext(ctx, "orphaned audio file after rollback",
			"audio_ref", ac.AudioRef, "error", err)
	}
	return nil
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
	if s == "" {
		return "Unknown"
	}
	forbidden := `<>:"/\|?*;`
	var b strings.Builder
	for _, r := range s {
		if !strings.ContainsRune(forbidden, r) {
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
