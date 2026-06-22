package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"altune/go-api/internal/acquisition/ports"
)

type StoreStep struct {
	audioStore ports.AudioWriter
}

func NewStoreStep(audioStore ports.AudioWriter) *StoreStep {
	return &StoreStep{audioStore: audioStore}
}

func (s *StoreStep) Name() string { return "store" }

func (s *StoreStep) Execute(ctx context.Context, ac *AcquisitionContext) error {
	if ac.TempPath == "" {
		return fmt.Errorf("no temp file to store")
	}

	audioRef := buildAudioRef(ac.Track)
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

func buildAudioRef(track TrackRef) string {
	artist := sanitizePathComponent(track.Artist)
	album := track.Album
	if album == "" {
		album = "Unknown Album"
	}
	album = sanitizePathComponent(album)
	title := sanitizePathComponent(track.Title)

	return strings.Join([]string{track.UserID, artist, album, title + ".mp3"}, "/")
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
