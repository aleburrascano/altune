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

// The scoring core: a forbidden source and a forbidden title each count as one
// contamination; recall is fraction-of-expected-present; coverage is
// fraction-of-albums-with-artwork-and-year.
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
			{Title: "Wrong One", Sources: []string{"deezer"}, HasArtwork: true, Year: 2019}, // forbidden source
		},
		tracks: []DetailItem{
			{Title: "Real Track", Sources: []string{"spotify"}},
			{Title: "Soul Song", Sources: []string{"applemusic"}}, // forbidden title
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

// A metadata-only control (no expected titles) must not inflate the recall
// averages — it's excluded from them, but still scored for coverage.
func TestRunDetailEval_controlExcludedFromRecall(t *testing.T) {
	goldens := []DetailGolden{
		{Name: "scored", SeedProvider: "deezer", SeedID: "1", ExpectedAlbums: []string{"a"}},
		{Name: "control", SeedProvider: "deezer", SeedID: "2"}, // no expected — control
	}
	svc := fakeDetailSvc{albums: []DetailItem{{Title: "B", HasArtwork: true, Year: 2021}}} // "a" absent → recall 0

	rep := RunDetailEval(context.Background(), goldens, svc)

	if rep.AlbumRecall != 0 {
		t.Errorf("album_recall = %.2f, want 0 (only the scored golden counts; the control's empty-expected 1.0 is excluded)", rep.AlbumRecall)
	}
}
