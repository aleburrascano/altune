package eval

import (
	"context"
	"testing"
)

type fakeDetailSvc struct {
	albums []DetailItem
	tracks []DetailItem
}

func (f fakeDetailSvc) Albums(context.Context, string, string, string) []DetailItem {
	return f.albums
}

func (f fakeDetailSvc) TopTracks(context.Context, string, string, string) []DetailItem {
	return f.tracks
}

func TestRunDetailEval_scores(t *testing.T) {
	goldens := []DetailGolden{{
		Name:              "X",
		SeedProvider:      "deezer",
		SeedID:            "1",
		ExpectedAlbums:    []string{"real album", "missing album"},
		ExpectedTopTracks: []string{"real track"},
		ForbiddenSources:  []string{"deezer"},
		ForbiddenTitles:   []string{"soul song"},
	}}
	svc := fakeDetailSvc{
		albums: []DetailItem{
			{Title: "Real Album (Deluxe)", Sources: []string{"spotify"}, HasArtwork: true, Year: 2020},
			{Title: "Wrong One", Sources: []string{"deezer"}, HasArtwork: true, Year: 2019},
		},
		tracks: []DetailItem{
			{Title: "Real Track", Sources: []string{"spotify"}},
			{Title: "Soul Song", Sources: []string{"applemusic"}},
		},
	}

	rep := RunDetailEval(context.Background(), goldens, svc)

	if rep.ContaminationCount != 2 {
		t.Errorf("contamination = %d, want 2 (deezer-sourced album + forbidden-title track)", rep.ContaminationCount)
	}
	if rep.AlbumRecall != 0.5 {
		t.Errorf("album_recall = %.2f, want 0.5 (real album present via substring, missing album absent)", rep.AlbumRecall)
	}
	if rep.TrackRecall != 1.0 {
		t.Errorf("track_recall = %.2f, want 1.0", rep.TrackRecall)
	}
	if rep.MetadataCoverage != 1.0 {
		t.Errorf("metadata_coverage = %.2f, want 1.0 (both albums carry artwork+year)", rep.MetadataCoverage)
	}
	if len(rep.Failures()) == 0 {
		t.Error("expected contamination + missing-recall failure records")
	}
}

func TestRunDetailEval_controlExcludedFromRecall(t *testing.T) {
	goldens := []DetailGolden{
		{Name: "scored", SeedProvider: "deezer", SeedID: "1", ExpectedAlbums: []string{"a"}},
		{Name: "control", SeedProvider: "deezer", SeedID: "2"},
	}
	svc := fakeDetailSvc{albums: []DetailItem{{Title: "B", HasArtwork: true, Year: 2021}}}

	rep := RunDetailEval(context.Background(), goldens, svc)

	if rep.AlbumRecall != 0 {
		t.Errorf("album_recall = %.2f, want 0 (only the scored golden counts; the control's empty-expected 1.0 is excluded)", rep.AlbumRecall)
	}
}

func TestDetailReport_MetricsDirections(t *testing.T) {
	r := DetailReport{ContaminationCount: 2, AlbumRecall: 0.9, TrackRecall: 0.8, MetadataCoverage: 0.7}
	m := metricByName(t, r.Metrics())
	if got := m["detail.contamination"]; got.Value != 2 || got.HigherIsBetter {
		t.Errorf("contamination = %+v, want 2 lower-is-better", got)
	}
	for name, want := range map[string]float64{
		"detail.album_recall":      0.9,
		"detail.track_recall":      0.8,
		"detail.metadata_coverage": 0.7,
	} {
		if got := m[name]; got.Value != want || !got.HigherIsBetter {
			t.Errorf("%s = %+v, want %v higher-is-better", name, got, want)
		}
	}
}

func TestRunDetailEval_EmptyGoldens(t *testing.T) {
	rep := RunDetailEval(context.Background(), nil, fakeDetailSvc{})
	if rep.Goldens != 0 || rep.AlbumRecall != 0 || rep.TrackRecall != 0 || rep.MetadataCoverage != 0 {
		t.Errorf("empty run must report zeros, got %+v", rep)
	}
	if len(rep.Failures()) != 0 {
		t.Errorf("empty run must have no failures, got %v", rep.Failures())
	}
}

func TestRunDetailEval_NoAlbumsExcludedFromCoverage(t *testing.T) {
	goldens := []DetailGolden{
		{Name: "empty", SeedProvider: "deezer", SeedID: "1"},
	}
	rep := RunDetailEval(context.Background(), goldens, fakeDetailSvc{})
	if rep.MetadataCoverage != 0 {
		t.Errorf("coverage = %v, want 0 (nothing measured)", rep.MetadataCoverage)
	}
	if rep.PerArtist[0].MetadataCoverage != 0 {
		t.Errorf("per-artist coverage = %v, want 0", rep.PerArtist[0].MetadataCoverage)
	}
}
