package downloadfromsearch

import (
	"altune/go-api/internal/download"
	"altune/go-api/internal/search"
	"context"
)

type musicBrainz interface {
	RecordingsByISRC(ctx context.Context, isrc string) ([]search.ISRCRecording, error)
	RecordingByMBID(ctx context.Context, mbid string) (search.MusicBrainzRecording, bool, error)
	WorkRecordings(ctx context.Context, recordingMBID string) ([]search.MusicBrainzWorkRecording, error)
	RecordingDetail(ctx context.Context, mbid string) (search.MusicBrainzRecordingDetail, bool, error)
}

type Recordings struct {
	musicBrainz musicBrainz
}

var (
	_ download.ISRCLookup        = Recordings{}
	_ download.RecordingLookup   = Recordings{}
	_ download.RerecordingLookup = Recordings{}
)

func NewRecordings(mb musicBrainz) Recordings {
	return Recordings{musicBrainz: mb}
}

func (r Recordings) RecordingsByISRC(ctx context.Context, isrc string) ([]download.ISRCRecording, error) {
	found, err := r.musicBrainz.RecordingsByISRC(ctx, isrc)
	if err != nil {
		return nil, err
	}
	recordings := make([]download.ISRCRecording, len(found))
	for i, rec := range found {
		recordings[i] = download.ISRCRecording{MBID: rec.MBID, Duration: float64(rec.Duration)}
	}
	return recordings, nil
}

func (r Recordings) RecordingByMBID(ctx context.Context, mbid string) (download.RecordingFacts, bool, error) {
	recording, found, err := r.musicBrainz.RecordingByMBID(ctx, mbid)
	return download.RecordingFacts(recording), found, err
}

func (r Recordings) WorkRecordings(ctx context.Context, recordingMBID string) ([]download.WorkRecording, error) {
	found, err := r.musicBrainz.WorkRecordings(ctx, recordingMBID)
	if err != nil {
		return nil, err
	}
	recordings := make([]download.WorkRecording, len(found))
	for i, rec := range found {
		recordings[i] = download.WorkRecording{
			MBID: rec.MBID, Title: rec.Title, Disambiguation: rec.Disambiguation, ArtistIDs: rec.ArtistIDs, Live: rec.Live,
		}
	}
	return recordings, nil
}

func (r Recordings) RecordingDetail(ctx context.Context, mbid string) (download.RecordingDetail, bool, error) {
	detail, found, err := r.musicBrainz.RecordingDetail(ctx, mbid)
	if err != nil || !found {
		return download.RecordingDetail{}, false, err
	}
	return download.RecordingDetail{
		MBID:             detail.MBID,
		Title:            detail.Title,
		ArtistIDs:        detail.ArtistIDs,
		ISRCs:            detail.ISRCs,
		Duration:         detail.Duration,
		FirstReleaseDate: detail.FirstReleaseDate,
		Releases:         releasesOf(detail.Releases),
	}, true, nil
}

func releasesOf(found []search.MusicBrainzRelease) []download.RecordingRelease {
	if len(found) == 0 {
		return nil
	}
	releases := make([]download.RecordingRelease, len(found))
	for i, rel := range found {
		releases[i] = download.RecordingRelease(rel)
	}
	return releases
}
