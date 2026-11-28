package service

import (
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/redact"
	"context"
	"errors"
	"fmt"
	"log/slog"
)

type storedFileFingerprinter struct {
	workspace  ports.ScratchFolder
	reader     ports.StoredAudioReader
	identifier ports.AudioIdentifier
}

func (f storedFileFingerprinter) fingerprint(ctx context.Context, ac *AcquisitionContext, track *ports.TrackSnapshot) {
	if track.AudioSourceURL != nil || track.AudioRef == nil {
		return
	}
	trackId := track.ID
	match, err := f.identify(ctx, *track.AudioRef, deref(track.StoredDurationSeconds))
	if errors.Is(err, ports.ErrNoStoredAudioReader) || errors.Is(err, ports.ErrNoAudioIdentifier) {
		warnCurrentFileUnidentified(ctx, trackId, "no stored-audio reader or identifier is wired")
		return
	}
	if err != nil {
		slog.WarnContext(ctx, "acquisition.replace_current_file_fingerprint_failed",
			logging.KeyTrackID, trackId.String(), "audio_ref", *track.AudioRef, logging.KeyError, redact.LogError(err))
		warnCurrentFileUnidentified(ctx, trackId, "the stored file could not be fingerprinted")
		return
	}
	if match.AcoustID == "" {
		warnCurrentFileUnidentified(ctx, trackId, "AcoustID returned no id for the stored file")
		return
	}
	ac.Replace.StoredAcoustID = match.AcoustID
	slog.InfoContext(ctx, "acquisition.replace_current_file_fingerprinted",
		logging.KeyTrackID, trackId.String(), "audio_ref", *track.AudioRef,
		"stored_acoustid", match.AcoustID, "known", match.Known())
}

func warnCurrentFileUnidentified(ctx context.Context, trackId ids.TrackId, reason string) {
	slog.WarnContext(ctx, "acquisition.replace_current_file_unidentified",
		logging.KeyTrackID, trackId.String(), "reason", reason,
		"effect", "no candidate is excluded as the current file")
}

func (f storedFileFingerprinter) identify(ctx context.Context, audioRef string, durationHint float64) (ports.RecordingMatch, error) {
	dir, err := f.workspace.Create("stored-*")
	if err != nil {
		return ports.RecordingMatch{}, fmt.Errorf("create stored-audio dir: %w", err)
	}
	defer removeScratch(ctx, f.workspace, dir)

	path, err := f.reader.CopyStored(ctx, audioRef, dir)
	if err != nil {
		return ports.RecordingMatch{}, fmt.Errorf("copy stored audio: %w", err)
	}
	return f.identifier.Identify(ctx, path, durationHint)
}
