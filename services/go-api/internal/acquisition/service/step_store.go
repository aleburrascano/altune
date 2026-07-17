package service

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"altune/go-api/internal/acquisition/ports"
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

// WithStoreProber validates that the final file decodes before it is persisted.
// This is the last gate — after download and tagging — so corruption introduced
// by any earlier step (a format-mismatched tagger, a truncated write) is caught
// before it reaches the library, not after a user tries to play it.
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

	audioRef := buildAudioRef(ac.Track, ac.TempPath)
	ac.AudioRef = audioRef

	if err := s.audioStore.Store(ctx, ac.TempPath, audioRef); err != nil {
		return fmt.Errorf("store audio: %w", err)
	}

	return nil
}

func (s *StoreStep) Rollback(ctx context.Context, ac *AcquisitionContext) error {
	if ac.AudioRef != "" {
		if err := s.audioStore.Delete(ctx, ac.AudioRef); err != nil {
			slog.ErrorContext(ctx, "orphaned audio file after rollback",
				"audio_ref", ac.AudioRef, "error", err)
		}
	}
	return nil
}

// buildAudioRef derives the ref's extension from the downloaded file itself, so
// the ref always names what the download actually produced. The format is
// decided once, by the searcher adapter — hardcoding it here again is how refs
// ended up lying about their bytes during the m4a era.
func buildAudioRef(track TrackRef, tempPath string) string {
	artist := sanitizePathComponent(track.Artist)
	album := track.Album
	if album == "" {
		album = "Unknown Album"
	}
	album = sanitizePathComponent(album)
	title := sanitizePathComponent(track.Title)

	ext := filepath.Ext(tempPath)
	if ext == "" {
		ext = ".mp3"
	}
	return strings.Join([]string{track.UserID, artist, album, title + ext}, "/")
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
	// A component made only of dots ("." / "..") is a path-traversal token; the
	// store defends against escapes, but acquisition should never emit one.
	if strings.Trim(result, ".") == "" {
		return "Unknown"
	}
	return result
}
