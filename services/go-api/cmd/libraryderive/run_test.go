package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeMusicBrainz struct {
	byISRC     map[string][]recording
	recordings map[string]recording
	homes      map[string][]releaseGroup
	blocked    bool
}

func (f *fakeMusicBrainz) RecordingsByISRC(_ context.Context, isrc string) ([]recording, error) {
	return f.byISRC[canonicalISRC(isrc)], nil
}

func (f *fakeMusicBrainz) Recording(_ context.Context, id string) (recording, error) {
	if f.blocked {
		return recording{}, blocked("HTTP 403")
	}
	rec, ok := f.recordings[id]
	if !ok {
		return recording{}, errCatalogUnknown
	}
	return rec, nil
}

func (f *fakeMusicBrainz) ReleaseGroups(_ context.Context, recordingID string) ([]releaseGroup, error) {
	return f.homes[recordingID], nil
}

func celebrationMusicBrainz() *fakeMusicBrainz {
	celebrate := group("rg-celebrate", "Celebrate!", "Album", "1980-10-01")
	celebrate.Artist = "Kool & The Gang"
	hits := group("rg-hits", "Greatest Hits of the 80's Vol 3", "Album", "1989", "Compilation")
	rec := recording{
		ID: "rec-celebration", Title: "Celebration", Artist: "Kool & The Gang", Primary: "Kool & The Gang",
		ISRCs: []string{celebrationISRC},
	}
	return &fakeMusicBrainz{
		byISRC:     map[string][]recording{celebrationISRC: {{ID: "rec-celebration", ISRCs: []string{celebrationISRC}}}},
		recordings: map[string]recording{"rec-celebration": rec},
		homes:      map[string][]releaseGroup{"rec-celebration": {hits, celebrate}},
	}
}

func celebrationTrack() boundTrack {
	return boundTrack{
		libraryTrack: libraryTrack{
			ID: "t1", UserID: "u1", Title: "Celebration", Artist: "Kool & The Gang", Album: "Greatest Hits of the 80's Vol 3",
			AlbumArtist: "Various Artists", ISRC: celebrationISRC, Year: 1989, Genre: "Pop", TrackNumber: 4,
			CatalogDurationSeconds: 221, ArtworkURL: "https://cdn/comp.jpg", FileSeconds: 298, Version: 7,
		},
		IdentityISRC: celebrationISRC,
	}
}

func byCategory(found []finding) map[category]finding {
	byKind := map[category]finding{}
	for _, f := range found {
		byKind[f.Category] = f
	}
	return byKind
}

func TestDeriveAll_CelebrationMovesToCelebrateWithTheAlbumVersionLength(t *testing.T) {
	d := newDeriver(celebrationMusicBrainz(), celebrationDeezer())
	got := d.deriveAll(t.Context(), []boundTrack{celebrationTrack()})
	byKind := byCategory(got.Findings)
	if got.Aborted != nil || got.Done != 1 {
		t.Fatalf("aborted %v done %d", got.Aborted, got.Done)
	}
	if !strings.HasPrefix(byKind[categoryReleaseChosen].Detail, "Celebrate! (Album, 1980-10-01) by studio_album; deezer same_isrc") {
		t.Fatalf("chosen: %+v", byKind[categoryReleaseChosen])
	}
	length := byKind["catalog_duration_seconds_correct"].Fix
	if length == nil || length.Field != "catalog_duration_seconds" || length.Mode != modeCorrect || *length.OldValue != "221" || *length.NewValue != "298" || length.UserID != "u1" || length.Version != 7 {
		t.Fatalf("length fix: %+v", length)
	}
	if byKind["year_correct"].Fix == nil || *byKind["year_correct"].Fix.NewValue != "1980" || byKind["album_differs"].Fix != nil {
		t.Fatalf("got %+v", got.Findings)
	}
}

func TestDeriveAll_AnotherMasterBecomesAFixIsrcPlanInTheRelengthShape(t *testing.T) {
	dz := celebrationDeezer()
	dz.byID[celebrateTrack] = deezerTrack{ID: celebrateTrack, ISRC: "USPR18000001", Title: "Celebration", DurationSeconds: 298}
	mb := celebrationMusicBrainz()
	rec := mb.recordings["rec-celebration"]
	rec.ISRCs = []string{celebrationISRC, "USPR18000001"}
	mb.recordings["rec-celebration"] = rec
	got := newDeriver(mb, dz).deriveAll(t.Context(), []boundTrack{celebrationTrack()})
	plan := byCategory(got.Findings)[categoryISRCOtherMaster].LengthPlan
	if plan == nil || *plan != (lengthPlan{Action: "fix_isrc", UserID: "u1", Version: 7, OldISRC: celebrationISRC, NewISRC: "USPR18000001", FileSeconds: 298, CatalogSeconds: 298}) {
		t.Fatalf("plan: %+v", plan)
	}
	for _, f := range got.Findings {
		if f.Fix != nil {
			t.Errorf("a track that changes master gets no field fixes, got %+v", f)
		}
	}
}

func TestDeriveAll_AnotherMasterThatDoesNotFitTheFileIsReportedWithoutAPlan(t *testing.T) {
	dz := celebrationDeezer()
	dz.byID[celebrateTrack] = deezerTrack{ID: celebrateTrack, ISRC: "USPR18000001", Title: "Celebration", DurationSeconds: 221}
	mb := celebrationMusicBrainz()
	rec := mb.recordings["rec-celebration"]
	rec.ISRCs = []string{celebrationISRC, "USPR18000001"}
	mb.recordings["rec-celebration"] = rec
	byKind := byCategory(newDeriver(mb, dz).deriveAll(t.Context(), []boundTrack{celebrationTrack()}).Findings)
	if byKind[categoryOtherMasterUnfit].Category == "" || byKind[categoryISRCOtherMaster].LengthPlan != nil {
		t.Fatalf("got %+v", byKind)
	}
}

func TestDeriveAll_NoDeezerAlbumStillDerivesYearAndAlbumArtistFromMusicBrainz(t *testing.T) {
	byKind := byCategory(newDeriver(celebrationMusicBrainz(), &fakeDeezer{}).deriveAll(t.Context(), []boundTrack{celebrationTrack()}).Findings)
	if !strings.Contains(byKind[categoryReleaseChosen].Detail, "deezer no_album") || byKind["year_correct"].Fix == nil || byKind["album_artist_correct"].Fix == nil {
		t.Fatalf("got %+v", byKind)
	}
	for _, kind := range []category{"track_number_correct", "genre_correct", "artwork_url_correct", "catalog_duration_seconds_correct"} {
		if _, wrote := byKind[kind]; wrote {
			t.Errorf("%s without a deezer album", kind)
		}
	}
}

func TestDeriveAll_UsesThePinnedMusicBrainzRecordingBeforeSearchingByIsrc(t *testing.T) {
	mb := celebrationMusicBrainz()
	mb.byISRC = nil
	track := celebrationTrack()
	track.IdentityISRC, track.MBRecordingID = "", "rec-celebration"
	got := newDeriver(mb, celebrationDeezer()).deriveAll(t.Context(), []boundTrack{track})
	if byCategory(got.Findings)[categoryReleaseChosen].Category == "" {
		t.Fatalf("got %+v", got.Findings)
	}
}

func TestDeriveAll_SkipsATrackItCannotAnchorWithAReason(t *testing.T) {
	ambiguous := celebrationMusicBrainz()
	ambiguous.byISRC[celebrationISRC] = []recording{{ID: "a", ISRCs: []string{celebrationISRC}}, {ID: "b", ISRCs: []string{celebrationISRC}}}
	onlyCompilations := celebrationMusicBrainz()
	onlyCompilations.homes["rec-celebration"] = []releaseGroup{
		group("rg-hits", "Hits", "Album", "1989", "Compilation"), group("rg-hits2", "Other Hits", "Album", "1991", "Compilation"),
	}
	noAnchor := celebrationTrack()
	noAnchor.ISRC, noAnchor.IdentityISRC = "", ""
	cases := []struct {
		name  string
		mb    *fakeMusicBrainz
		track boundTrack
		want  category
	}{
		{"no isrc and no recording id", celebrationMusicBrainz(), noAnchor, categoryNoAnchor},
		{"two recordings carry the isrc", ambiguous, celebrationTrack(), categoryRecordingAmbiguous},
		{"musicbrainz does not know the isrc", &fakeMusicBrainz{}, celebrationTrack(), categoryRecordingUnknown},
		{"only compilations", onlyCompilations, celebrationTrack(), categoryReleaseUnchosen},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := newDeriver(tc.mb, celebrationDeezer()).deriveAll(t.Context(), []boundTrack{tc.track})
			if len(got.Findings) != 1 || got.Findings[0].Category != tc.want || got.Findings[0].Fix != nil {
				t.Fatalf("got %+v", got.Findings)
			}
		})
	}
}

func TestDeriveAll_ABlockedDeezerAbortsTheRun(t *testing.T) {
	dz := celebrationDeezer()
	dz.blocked = true
	got := newDeriver(celebrationMusicBrainz(), dz).deriveAll(t.Context(), []boundTrack{celebrationTrack(), celebrationTrack()})
	if got.Done != 0 || len(got.Findings) != 0 || !errors.Is(got.Aborted, errCatalogBlocked) {
		t.Fatalf("done %d aborted %v", got.Done, got.Aborted)
	}
}

func TestDeriveAll_ABlockedMusicBrainzAbortsTheRun(t *testing.T) {
	mb := celebrationMusicBrainz()
	mb.blocked = true
	got := newDeriver(mb, celebrationDeezer()).deriveAll(t.Context(), []boundTrack{celebrationTrack()})
	if got.Done != 0 || !errors.Is(got.Aborted, errCatalogBlocked) {
		t.Fatalf("done %d aborted %v", got.Done, got.Aborted)
	}
}

func TestBuildReport_EmitsTheShapesReconcileAndRelengthRead(t *testing.T) {
	got := newDeriver(celebrationMusicBrainz(), celebrationDeezer()).deriveAll(t.Context(), []boundTrack{celebrationTrack()})
	var encoded strings.Builder
	if err := writeReport(&encoded, buildReport(1, got.Findings, "", time.Unix(0, 0))); err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Aborted  bool `json:"aborted"`
		Findings []struct {
			TrackID  string `json:"track_id"`
			Category string `json:"category"`
			Fix      *struct {
				UserID   string  `json:"user_id"`
				Version  int     `json:"version"`
				Field    string  `json:"field"`
				Mode     string  `json:"mode"`
				OldValue *string `json:"old_value"`
				NewValue *string `json:"new_value"`
			} `json:"fix"`
		} `json:"findings"`
	}
	if err := json.Unmarshal([]byte(encoded.String()), &parsed); err != nil {
		t.Fatal(err)
	}
	var corrections int
	for _, f := range parsed.Findings {
		if f.Fix != nil && f.Fix.Mode == "correct" && f.Fix.OldValue != nil && f.Fix.NewValue != nil && f.Fix.UserID == "u1" && f.Fix.Version == 7 && f.TrackID == "t1" {
			corrections++
		}
	}
	if parsed.Aborted || corrections == 0 {
		t.Fatalf("report: %s", encoded.String())
	}
}
