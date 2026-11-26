package orphans

import (
	"altune/go-api/internal/library"
	"context"
	"errors"
	"fmt"
)

const missingAudioReason = "Audio file missing from storage (health-check)"

func Check(ctx context.Context, tracks []library.ReadyAudioTrack, store library.AudioStore) []library.ReadyAudioTrack {
	var orphaned []library.ReadyAudioTrack
	for _, t := range tracks {
		exists, err := store.Exists(ctx, t.AudioRef)
		if err == nil && exists {
			continue
		}
		orphaned = append(orphaned, t)
		fmt.Printf("  ORPHANED: %s — %s  (id=%s, ref=%s)\n", t.Title, t.Artist, t.Id, t.AudioRef)
	}
	return orphaned
}

func Fix(ctx context.Context, orphaned []library.ReadyAudioTrack, repo library.TrackReadWriter) (fixed int) {
	for _, t := range orphaned {
		if err := markTrackFailed(ctx, repo, t); err != nil {
			fmt.Printf("    → NOT FIXED: %s: %v\n", t.Id, err)
			continue
		}
		fixed++
		fmt.Printf("    → FIXED: %s marked as failed\n", t.Id)
	}
	return fixed
}

func RepairOutcome(orphaned, fixed int) error {
	if fixed >= orphaned {
		return nil
	}
	return fmt.Errorf("%d of %d orphaned tracks could not be marked failed", orphaned-fixed, orphaned)
}

func markTrackFailed(ctx context.Context, repo library.TrackReadWriter, t library.ReadyAudioTrack) error {
	track, err := repo.GetByID(ctx, t.Id, t.UserId)
	if err != nil {
		return err
	}
	if track == nil {
		return errors.New("track disappeared")
	}
	if err := track.MarkFailed(missingAudioReason); err != nil {
		return err
	}
	return repo.Update(ctx, track, track.Version)
}
