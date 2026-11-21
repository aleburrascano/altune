package orphans

import (
	"altune/go-api/internal/catalog"
	"context"
	"errors"
	"fmt"
)

const missingAudioReason = "Audio file missing from storage (health-check)"

func Check(ctx context.Context, tracks []catalog.ReadyAudioTrack, store catalog.AudioStore, repo catalog.TrackReadWriter, fix bool) (orphaned, fixed int) {
	for _, t := range tracks {
		exists, err := store.Exists(ctx, t.AudioRef)
		if err == nil && exists {
			continue
		}
		orphaned++
		fmt.Printf("  ORPHANED: %s — %s  (id=%s, ref=%s)\n", t.Title, t.Artist, t.Id, t.AudioRef)

		if fix {
			_ = markTrackFailed(ctx, repo, t)
			fixed++
			fmt.Println("    → FIXED: marked as failed")
		}
	}
	return orphaned, fixed
}

func markTrackFailed(ctx context.Context, repo catalog.TrackReadWriter, t catalog.ReadyAudioTrack) error {
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
