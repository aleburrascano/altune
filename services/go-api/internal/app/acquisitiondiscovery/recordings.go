package acquisitiondiscovery

import (
	"altune/go-api/internal/acquisition"
	"altune/go-api/internal/discovery"
	"context"
)

type musicBrainz interface {
	RecordingsByISRC(ctx context.Context, isrc string) ([]discovery.ISRCRecording, error)
	RecordingByMBID(ctx context.Context, mbid string) (discovery.MBRecording, bool, error)
	WorkRecordings(ctx context.Context, recordingMBID string) ([]discovery.MBWorkRecording, error)
	RecordingDetail(ctx context.Context, mbid string) (discovery.MBRecordingDetail, bool, error)
}

type Recordings struct {
	musicBrainz musicBrainz
}

var (
	_ acquisition.ISRCAuthority        = Recordings{}
	_ acquisition.RecordingLookup      = Recordings{}
	_ acquisition.RerecordingAuthority = Recordings{}
)

func NewRecordings(mb musicBrainz) Recordings {
	return Recordings{musicBrainz: mb}
}

func (r Recordings) RecordingsByISRC(ctx context.Context, isrc string) ([]acquisition.ISRCRecording, error) {
	found, err := r.musicBrainz.RecordingsByISRC(ctx, isrc)
	if err != nil {
		return nil, err
	}
	recordings := make([]acquisition.ISRCRecording, len(found))
	for i, rec := range found {
		recordings[i] = acquisition.ISRCRecording{MBID: rec.MBID, Duration: float64(rec.Duration)}
	}
	return recordings, nil
}

func (r Recordings) RecordingByMBID(ctx context.Context, mbid string) (acquisition.RecordingFacts, bool, error) {
	recording, found, err := r.musicBrainz.RecordingByMBID(ctx, mbid)
	return acquisition.RecordingFacts(recording), found, err
}

func (r Recordings) WorkRecordings(ctx context.Context, recordingMBID string) ([]acquisition.WorkRecording, error) {
	found, err := r.musicBrainz.WorkRecordings(ctx, recordingMBID)
	if err != nil {
		return nil, err
	}
	recordings := make([]acquisition.WorkRecording, len(found))
	for i, rec := range found {
		recordings[i] = acquisition.WorkRecording{
			MBID: rec.MBID, Title: rec.Title, Disambiguation: rec.Disambiguation, ArtistIDs: rec.ArtistIDs, Live: rec.Live,
		}
	}
	return recordings, nil
}

func (r Recordings) RecordingDetail(ctx context.Context, mbid string) (acquisition.RecordingDetail, bool, error) {
	detail, found, err := r.musicBrainz.RecordingDetail(ctx, mbid)
	if err != nil || !found {
		return acquisition.RecordingDetail{}, false, err
	}
	return acquisition.RecordingDetail{
		MBID:             detail.MBID,
		Title:            detail.Title,
		ArtistIDs:        detail.ArtistIDs,
		ISRCs:            detail.ISRCs,
		Duration:         detail.Duration,
		FirstReleaseDate: detail.FirstReleaseDate,
		Releases:         releasesOf(detail.Releases),
	}, true, nil
}

func releasesOf(found []discovery.MBRelease) []acquisition.RecordingRelease {
	if len(found) == 0 {
		return nil
	}
	releases := make([]acquisition.RecordingRelease, len(found))
	for i, rel := range found {
		releases[i] = acquisition.RecordingRelease(rel)
	}
	return releases
}
