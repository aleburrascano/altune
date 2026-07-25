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
	if strings.Trim(result, ".") == "" {
		return "Unknown"
	}
	return result
}
