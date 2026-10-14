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
	lengths    map[string]float64
	lengthErr  error
	blocked    bool
}

func (f *fakeMusicBrainz) ReleaseTrackSeconds(_ context.Context, releaseID, recordingID string) (float64, error) {
	if f.lengthErr != nil {
		return 0, f.lengthErr
	}
	seconds, ok := f.lengths[releaseID+"/"+recordingID]
	if !ok {
		return 0, errCatalogUnknown
	}
	return seconds, nil
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
	return celebrationReleasedAs("", "")
}

func celebrationReleasedAs(releaseID, frontReleaseID string) *fakeMusicBrainz {
	celebrate := group("rg-celebrate", "Celebrate!", "Album", "1980-10-01")
	celebrate.Artist = "Kool & The Gang"
	celebrate.ReleaseID, celebrate.FrontReleaseID = releaseID, frontReleaseID
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

func TestDeriveAll_AnotherMasterBecomesAFixIsrcPlanInTheRelengthShapeAndEveryOtherFieldIsStillWritten(t *testing.T) {
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
		if (f.Category == categoryISRCOtherMaster || strings.HasSuffix(string(f.Category), "_differs")) && f.Fix != nil {
			t.Errorf("the ISRC change is a plan, not a field fix, got %+v", f)
		}
	}
	if album := byCategory(got.Findings)["album_correct"].Fix; album == nil || *album.NewValue != "Celebrate!" {
		t.Errorf("a track that changes master still gets its other fields written, got %+v", got.Findings)
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

func celebrateOnMusicBrainz() *fakeMusicBrainz {
	mb := celebrationReleasedAs("rel-celebrate", "rel-celebrate-front")
	mb.lengths = map[string]float64{"rel-celebrate-front/rec-celebration": 298, "rel-celebrate/rec-celebration": 300}
	return mb
}

func TestDeriveAll_WithoutADeezerAlbumTheCoverAndLengthComeFromTheChosenMusicBrainzRelease(t *testing.T) {
	byKind := byCategory(newDeriver(celebrateOnMusicBrainz(), &fakeDeezer{}).deriveAll(t.Context(), []boundTrack{celebrationTrack()}).Findings)
	cover, length := byKind["artwork_url_correct"].Fix, byKind["catalog_duration_seconds_correct"].Fix
	if cover == nil || *cover.OldValue != "https://cdn/comp.jpg" || *cover.NewValue != "https://coverartarchive.org/release/rel-celebrate-front/front" {
		t.Fatalf("cover fix: %+v", cover)
	}
	if length == nil || *length.OldValue != "221" || *length.NewValue != "298" {
		t.Fatalf("length fix: %+v", length)
	}
}

func TestDeriveAll_TheDeezerAlbumOfTheChosenReleaseOutranksMusicBrainzForCoverAndLength(t *testing.T) {
	mb := celebrateOnMusicBrainz()
	mb.lengths["rel-celebrate-front/rec-celebration"] = 100
	byKind := byCategory(newDeriver(mb, celebrationDeezer()).deriveAll(t.Context(), []boundTrack{celebrationTrack()}).Findings)
	if *byKind["artwork_url_correct"].Fix.NewValue != "https://cdn/celebrate.jpg" || *byKind["catalog_duration_seconds_correct"].Fix.NewValue != "298" {
		t.Fatalf("got %+v", byKind)
	}
}

func TestDeriveAll_AChosenReleaseWithoutFrontArtOrATrackLengthFillsNothing(t *testing.T) {
	mb := celebrationReleasedAs("rel-celebrate", "")
	byKind := byCategory(newDeriver(mb, &fakeDeezer{}).deriveAll(t.Context(), []boundTrack{celebrationTrack()}).Findings)
	for _, kind := range []category{"artwork_url_correct", "catalog_duration_seconds_correct"} {
		if _, wrote := byKind[kind]; wrote {
			t.Errorf("%s without a release cover or length", kind)
		}
	}
}

func TestDeriveAll_AFailedTracklistLookupKeepsTheCoverAndOnlyLosesTheLength(t *testing.T) {
	mb := celebrateOnMusicBrainz()
	mb.lengthErr = errors.New("HTTP 503")
	byKind := byCategory(newDeriver(mb, &fakeDeezer{}).deriveAll(t.Context(), []boundTrack{celebrationTrack()}).Findings)
	if cover := byKind["artwork_url_correct"].Fix; cover == nil || *cover.NewValue != "https://coverartarchive.org/release/rel-celebrate-front/front" {
		t.Fatalf("cover fix: %+v", byKind)
	}
	if _, wrote := byKind["catalog_duration_seconds_correct"]; wrote {
		t.Fatalf("length written after a failed lookup: %+v", byKind)
	}
}

func TestDeriveAll_ABlockedTracklistLookupAbortsTheRun(t *testing.T) {
	mb := celebrateOnMusicBrainz()
	mb.lengthErr = blocked("HTTP 503")
	if out := newDeriver(mb, &fakeDeezer{}).deriveAll(t.Context(), []boundTrack{celebrationTrack()}); !errors.Is(out.Aborted, errCatalogBlocked) {
		t.Fatalf("got %+v", out)
	}
}

type fakeCoverProbe struct {
	placeholders map[string]bool
	dead         map[string]bool
	unreachable  map[string]bool
}

func (f fakeCoverProbe) IsPlaceholder(_ context.Context, coverURL string) (bool, error) {
	if f.unreachable[coverURL] {
		return false, errors.New("cdn timeout")
	}
	return f.placeholders[coverURL], nil
}

func (f fakeCoverProbe) Resolves(_ context.Context, coverURL string) (bool, error) {
	return !f.dead[coverURL], nil
}

const (
	celebrateDeezerCover = "https://cdn/celebrate.jpg"
	celebrateArtCover    = "https://coverartarchive.org/release/rel-celebrate-front/front"
)

func deriveCelebrationWith(t *testing.T, mb *fakeMusicBrainz, dz *fakeDeezer, probe fakeCoverProbe, track boundTrack) map[category]finding {
	d := newDeriver(mb, dz)
	d.covers = probe
	return byCategory(d.deriveAll(t.Context(), []boundTrack{track}).Findings)
}

func TestDeriveAll_ADeezerCoverThatIsTheBlankPlaceholderFallsBackToTheCoverArtArchive(t *testing.T) {
	probe := fakeCoverProbe{placeholders: map[string]bool{celebrateDeezerCover: true}}
	cover := deriveCelebrationWith(t, celebrateOnMusicBrainz(), celebrationDeezer(), probe, celebrationTrack())["artwork_url_correct"].Fix
	if cover == nil || *cover.NewValue != celebrateArtCover {
		t.Fatalf("cover fix: %+v", cover)
	}
}

func TestDeriveAll_ABlankDeezerCoverWithoutACoverArtArchiveFrontEmitsNoArtworkFix(t *testing.T) {
	probe := fakeCoverProbe{placeholders: map[string]bool{celebrateDeezerCover: true}}
	got := deriveCelebrationWith(t, celebrationReleasedAs("rel-celebrate", ""), celebrationDeezer(), probe, celebrationTrack())
	if _, wrote := got["artwork_url_correct"]; wrote {
		t.Errorf("a placeholder was written: %+v", got["artwork_url_correct"])
	}
	if detail := got["artwork_url_differs"].Detail; !strings.Contains(detail, "blank placeholder") {
		t.Errorf("the unwritten cover is not explained: %q", detail)
	}
}

func TestDeriveAll_ACoverArtArchiveFrontThatDoesNotResolveEmitsNoArtworkFix(t *testing.T) {
	probe := fakeCoverProbe{dead: map[string]bool{celebrateArtCover: true}}
	got := deriveCelebrationWith(t, celebrateOnMusicBrainz(), &fakeDeezer{}, probe, celebrationTrack())
	if _, wrote := got["artwork_url_correct"]; wrote {
		t.Fatalf("a dead cover was written: %+v", got["artwork_url_correct"])
	}
	if got["catalog_duration_seconds_correct"].Fix == nil {
		t.Fatalf("the length fix was lost with the cover: %+v", got)
	}
}

func TestDeriveAll_AStoredBlankPlaceholderIsReplacedEvenWhenTheAlbumStaysTheSame(t *testing.T) {
	track := celebrationTrack()
	track.Album, track.AlbumArtist = "Celebrate!", "Kool & The Gang"
	placeholder := fakeCoverProbe{placeholders: map[string]bool{track.ArtworkURL: true}}
	cover := deriveCelebrationWith(t, celebrateOnMusicBrainz(), celebrationDeezer(), placeholder, track)["artwork_url_correct"].Fix
	if cover == nil || *cover.OldValue != "https://cdn/comp.jpg" || *cover.NewValue != celebrateDeezerCover || cover.Version != 7 {
		t.Fatalf("cover fix: %+v", cover)
	}
}

func TestDeriveAll_WhenDeezerHasNoAlbumTheUnwrittenFieldsAreListedWithTheirReason(t *testing.T) {
	probe := fakeCoverProbe{dead: map[string]bool{celebrateArtCover: true}}
	got := deriveCelebrationWith(t, celebrateOnMusicBrainz(), &fakeDeezer{}, probe, celebrationTrack())
	reasons := map[category]string{
		"genre_differs":        "no deezer album matches the chosen release",
		"track_number_differs": "no deezer album matches the chosen release",
		"artwork_url_differs":  "the cover art archive front of the chosen release does not resolve",
	}
	for kind, want := range reasons {
		if detail := got[kind].Detail; !strings.Contains(detail, want) {
			t.Errorf("%s detail %q lacks %q", kind, detail, want)
		}
	}
}

func TestDeriveAll_AnotherMasterStillWritesTheTracksOtherFieldsAndCorrectsTheISRCToTheBoundOne(t *testing.T) {
	dz := celebrationDeezer()
	dz.byID[celebrateTrack] = deezerTrack{ID: celebrateTrack, ISRC: "USPR18000001", Title: "Celebration", DurationSeconds: 298}
	mb := celebrateOnMusicBrainz()
	rec := mb.recordings["rec-celebration"]
	rec.ISRCs = []string{celebrationISRC, "USPR18000001"}
	mb.recordings["rec-celebration"] = rec
	track := celebrationTrack()
	track.ISRC = "USPR18000009"
	got := deriveCelebrationWith(t, mb, dz, fakeCoverProbe{}, track)
	isrc := got["isrc_correct"].Fix
	if got["year_correct"].Fix == nil || got["album_correct"].Fix == nil || isrc == nil || *isrc.NewValue != celebrationISRC || *isrc.OldValue != "USPR18000009" {
		t.Fatalf("got %+v", got)
	}
	if got[categoryISRCOtherMaster].Category == "" && got[categoryOtherMasterUnfit].Category == "" {
		t.Fatalf("the master change was dropped: %+v", got)
	}
}

func TestDeriveAll_AStoredCoverThatCannotBeCheckedIsReportedAndNeverOverwritten(t *testing.T) {
	track := celebrationTrack()
	track.Album, track.AlbumArtist = "Celebrate!", "Kool & The Gang"
	probe := fakeCoverProbe{unreachable: map[string]bool{track.ArtworkURL: true}}
	got := deriveCelebrationWith(t, celebrateOnMusicBrainz(), celebrationDeezer(), probe, track)
	if _, wrote := got["artwork_url_correct"]; wrote {
		t.Fatalf("a cover that could not be checked was overwritten: %+v", got["artwork_url_correct"])
	}
	if detail := got["artwork_url_differs"].Detail; !strings.Contains(detail, "stored cover could not be checked") {
		t.Fatalf("detail %q", detail)
	}
}

func TestDeriveAll_ADeezerCoverThatCannotBeCheckedIsReportedWithoutAFixOrAFallback(t *testing.T) {
	probe := fakeCoverProbe{unreachable: map[string]bool{celebrateDeezerCover: true}}
	got := deriveCelebrationWith(t, celebrateOnMusicBrainz(), celebrationDeezer(), probe, celebrationTrack())
	if _, wrote := got["artwork_url_correct"]; wrote {
		t.Fatalf("an unchecked cover was written: %+v", got["artwork_url_correct"])
	}
	if detail := got["artwork_url_differs"].Detail; !strings.Contains(detail, "deezer cover could not be checked") {
		t.Fatalf("detail %q", detail)
	}
}

func TestDeriveAll_ACoverUrlThatAlreadyNamesThePlaceholderImageIsBlankWithoutARequest(t *testing.T) {
	track := celebrationTrack()
	track.ArtworkURL = "https://cdn/images/cover/" + deezerPlaceholderImageID + "/1000x1000.jpg"
	probe := fakeCoverProbe{unreachable: map[string]bool{track.ArtworkURL: true}}
	cover := deriveCelebrationWith(t, celebrateOnMusicBrainz(), celebrationDeezer(), probe, track)["artwork_url_correct"].Fix
	if cover == nil || *cover.OldValue != track.ArtworkURL || *cover.NewValue != celebrateDeezerCover {
		t.Fatalf("cover fix: %+v", cover)
	}
}

func TestDeriveAll_AnIsrcNeitherOnTheRecordingNorConfirmedByABindIsReportedNotCorrected(t *testing.T) {
	track := celebrationTrack()
	track.ISRC, track.IdentityISRC, track.MBRecordingID = "USPR18000009", "USPR18000001", "rec-celebration"
	unproven := deriveCelebrationWith(t, celebrateOnMusicBrainz(), celebrationDeezer(), fakeCoverProbe{}, track)
	if _, wrote := unproven["isrc_correct"]; wrote || !strings.Contains(unproven["isrc_differs"].Detail, "not listed on the musicbrainz recording") {
		t.Fatalf("got %+v", unproven)
	}
	track.CatalogConfirmed = true
	if proven := deriveCelebrationWith(t, celebrateOnMusicBrainz(), celebrationDeezer(), fakeCoverProbe{}, track); proven["isrc_correct"].Fix == nil {
		t.Fatalf("a bind-confirmed isrc was not corrected: %+v", proven)
	}
}
