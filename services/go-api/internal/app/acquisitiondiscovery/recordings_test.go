package acquisitiondiscovery_test

import (
	"altune/go-api/internal/acquisition"
	"altune/go-api/internal/app/acquisitiondiscovery"
	"altune/go-api/internal/discovery"
	"context"
	"errors"
	"reflect"
	"testing"
)

type stubMusicBrainz struct {
	isrc      []discovery.ISRCRecording
	recording discovery.MBRecording
	work      []discovery.MBWorkRecording
	detail    discovery.MBRecordingDetail
	found     bool
	err       error
	asked     string
}

func (s *stubMusicBrainz) RecordingsByISRC(_ context.Context, isrc string) ([]discovery.ISRCRecording, error) {
	s.asked = isrc
	return s.isrc, s.err
}

func (s *stubMusicBrainz) RecordingByMBID(_ context.Context, mbid string) (discovery.MBRecording, bool, error) {
	s.asked = mbid
	return s.recording, s.found, s.err
}

func (s *stubMusicBrainz) WorkRecordings(_ context.Context, mbid string) ([]discovery.MBWorkRecording, error) {
	s.asked = mbid
	return s.work, s.err
}

func (s *stubMusicBrainz) RecordingDetail(_ context.Context, mbid string) (discovery.MBRecordingDetail, bool, error) {
	s.asked = mbid
	return s.detail, s.found, s.err
}

const cureLiveMBID = "95ccd64c-5b38-43e0-827d-bb14b271372a"

func TestRecordingsByISRC_CarriesEachRecordingAndItsLength(t *testing.T) {
	mb := &stubMusicBrainz{isrc: []discovery.ISRCRecording{{MBID: "single-edit", Duration: 220}, {MBID: "album", Duration: 237}}}

	got, err := acquisitiondiscovery.NewRecordings(mb).RecordingsByISRC(context.Background(), "CAA509814003")

	want := []acquisition.ISRCRecording{{MBID: "single-edit", Duration: 220}, {MBID: "album", Duration: 237}}
	if err != nil || !reflect.DeepEqual(got, want) || mb.asked != "CAA509814003" {
		t.Errorf("recordings %+v, err %v, asked %q; want %+v for the asked ISRC", got, err, mb.asked, want)
	}
}

func TestRecordingByMBID_CarriesEveryFactAcrossTheDiscoveryBoundary(t *testing.T) {
	mb := &stubMusicBrainz{found: true, recording: discovery.MBRecording{
		MBID: cureLiveMBID, Title: "Boys Don’t Cry", Disambiguation: "live, 1991-03-03: The Limehouse, London, UK",
		Artists: []string{"The Cure"}, ISRCs: []string{"GBAAA8000001"}, Duration: 187,
	}}

	facts, found, err := acquisitiondiscovery.NewRecordings(mb).RecordingByMBID(context.Background(), cureLiveMBID)

	want := acquisition.RecordingFacts{
		MBID: cureLiveMBID, Title: "Boys Don’t Cry", Disambiguation: "live, 1991-03-03: The Limehouse, London, UK",
		Artists: []string{"The Cure"}, ISRCs: []string{"GBAAA8000001"}, Duration: 187,
	}
	if err != nil || !found || !reflect.DeepEqual(facts, want) || mb.asked != cureLiveMBID {
		t.Errorf("facts %+v found %v err %v asked %q; want %+v", facts, found, err, mb.asked, want)
	}
}

func TestWorkRecordings_CarriesEveryFactTheRerecordingSearchReads(t *testing.T) {
	mb := &stubMusicBrainz{work: []discovery.MBWorkRecording{{
		MBID: "classic", Title: "It’s Only Love (classic version)", Disambiguation: "2025 re-recording",
		ArtistIDs: []string{"bryan-adams"}, Duration: 194, Live: true,
	}}}

	got, err := acquisitiondiscovery.NewRecordings(mb).WorkRecordings(context.Background(), "original")

	want := []acquisition.WorkRecording{{
		MBID: "classic", Title: "It’s Only Love (classic version)", Disambiguation: "2025 re-recording",
		ArtistIDs: []string{"bryan-adams"}, Live: true,
	}}
	if err != nil || !reflect.DeepEqual(got, want) || mb.asked != "original" {
		t.Errorf("work %+v, err %v, asked %q; want %+v", got, err, mb.asked, want)
	}
}

func TestRecordingDetail_CarriesEveryFactAndRelease(t *testing.T) {
	mb := &stubMusicBrainz{found: true, detail: discovery.MBRecordingDetail{
		MBID: "classic", Title: "It’s Only Love (classic version)", ArtistIDs: []string{"bryan-adams"},
		ISRCs: []string{"IETLH2500023"}, Duration: 194, FirstReleaseDate: "2025-04-25",
		Releases: []discovery.MBRelease{{Title: "It’s Only Love", Date: "2025-04-25", Status: "Official"}},
	}}

	got, found, err := acquisitiondiscovery.NewRecordings(mb).RecordingDetail(context.Background(), "classic")

	want := acquisition.RecordingDetail{
		MBID: "classic", Title: "It’s Only Love (classic version)", ArtistIDs: []string{"bryan-adams"},
		ISRCs: []string{"IETLH2500023"}, Duration: 194, FirstReleaseDate: "2025-04-25",
		Releases: []acquisition.RecordingRelease{{Title: "It’s Only Love", Date: "2025-04-25", Status: "Official"}},
	}
	if err != nil || !found || !reflect.DeepEqual(got, want) || mb.asked != "classic" {
		t.Errorf("detail %+v found %v err %v asked %q; want %+v", got, found, err, mb.asked, want)
	}
}

func TestRecordings_PassNotFoundThrough(t *testing.T) {
	recordings := acquisitiondiscovery.NewRecordings(&stubMusicBrainz{recording: discovery.MBRecording{MBID: "stale"}})

	if _, found, err := recordings.RecordingByMBID(context.Background(), cureLiveMBID); found || err != nil {
		t.Errorf("unknown recording = found %v err %v; want not found without error", found, err)
	}
	if detail, found, err := recordings.RecordingDetail(context.Background(), cureLiveMBID); found || err != nil || detail.MBID != "" {
		t.Errorf("unknown detail = %+v found %v err %v; want an empty not found", detail, found, err)
	}
}

func TestRecordings_PassFailuresThrough(t *testing.T) {
	boom := errors.New("503")
	recordings := acquisitiondiscovery.NewRecordings(&stubMusicBrainz{
		err: boom, found: true,
		isrc: []discovery.ISRCRecording{{MBID: "partial"}}, work: []discovery.MBWorkRecording{{MBID: "partial"}},
	})
	ctx := context.Background()

	if got, err := recordings.RecordingsByISRC(ctx, "isrc"); !errors.Is(err, boom) || got != nil {
		t.Errorf("RecordingsByISRC = %+v, %v; want only the failure", got, err)
	}
	if _, _, err := recordings.RecordingByMBID(ctx, "mbid"); !errors.Is(err, boom) {
		t.Errorf("RecordingByMBID err = %v, want the failure", err)
	}
	if got, err := recordings.WorkRecordings(ctx, "mbid"); !errors.Is(err, boom) || got != nil {
		t.Errorf("WorkRecordings = %+v, %v; want only the failure", got, err)
	}
	if got, found, err := recordings.RecordingDetail(ctx, "mbid"); !errors.Is(err, boom) || found || got.MBID != "" {
		t.Errorf("RecordingDetail = %+v, %v, %v; want only the failure", got, found, err)
	}
}
