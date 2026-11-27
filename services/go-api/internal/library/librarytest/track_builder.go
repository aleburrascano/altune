package librarytest

import "altune/go-api/internal/library/internal/domain"

type TrackSnapshot = domain.TrackSnapshot

func RestoreTrack(s TrackSnapshot) *domain.Track { return domain.RestoreTrack(s) }

func AmendTrack(track *domain.Track, change func(*TrackSnapshot)) *domain.Track {
	snapshot := track.Snapshot()
	change(&snapshot)
	*track = *domain.RestoreTrack(snapshot)
	return track
}
