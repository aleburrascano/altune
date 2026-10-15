package main

import (
	"altune/go-api/internal/shared/songfacts"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

type fakeMusicBrainz struct {
	byISRC     map[string][]recording
	recordings map[string]recording
	homes      map[string][]releaseGroup
	lengths    map[string]float64
	positions  map[string]int
	lengthErr  error
	blocked    bool
}

func (f *fakeMusicBrainz) ReleaseTrack(_ context.Context, releaseID, recordingID string) (releaseTrack, error) {
	if f.lengthErr != nil {
		return releaseTrack{}, f.lengthErr
	}
	key := releaseID + "/" + recordingID
	seconds, ok := f.lengths[key]
	if !ok {
		return releaseTrack{}, errCatalogUnknown
	}
	return releaseTrack{Seconds: seconds, Position: f.positions[key]}, nil
}

func (f *fakeMusicBrainz) RecordingsByISRC(_ context.Context, isrc string) ([]recording, error) {
	return f.byISRC[songfacts.CanonicalISRC(isrc)], nil
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

func deriveSong(t *testing.T, rec recording, homes []releaseGroup, dz *fakeDeezer, stored libraryTrack) map[category]finding {
	t.Helper()
	mb := &fakeMusicBrainz{recordings: map[string]recording{rec.ID: rec}, homes: map[string][]releaseGroup{rec.ID: homes}}
	bound := boundTrack{libraryTrack: stored, MBRecordingID: rec.ID, IdentityISRC: stored.ISRC}
	return byCategory(newDeriver(mb, dz).deriveAll(t.Context(), []boundTrack{bound}).Findings)
}

func deezerCrediting(isrc, title, artist, albumTitle string) *fakeDeezer {
	return &fakeDeezer{
		byISRC: map[string]deezerTrack{
			isrc: {ID: "1", ISRC: isrc, Title: title, Artist: artist, Position: 1, DurationSeconds: 200, AlbumID: "10", AlbumTitle: albumTitle},
		},
		albums: map[string]deezerAlbum{"10": {ID: "10", Title: albumTitle, CoverURL: "https://cdn/a.jpg", Genre: "Pop"}},
	}
}

func TestDeriveAll_ATitleMusicBrainzSpellsDifferentlyIsReportedUnlessDeezerCarriesIt(t *testing.T) {
	cases := []struct {
		artist, album, stored, musicBrainz, isrc string
	}{
		{"BLACKPINK", "BORN PINK", "JUMP", "뛰어 (JUMP)", "KRA302200001"},
		{"Jon Secada", "Jon Secada", "Angel", "Ángel", "USEM39200001"},
		{"Cordae", "The Lost Boy", "C Carter", "Coach Carter", "USRC11900001"},
	}
	for _, c := range cases {
		t.Run(c.stored, func(t *testing.T) {
			rec := recording{ID: "rec", Title: c.musicBrainz, Artist: c.artist, Primary: c.artist, ISRCs: []string{c.isrc}}
			homes := []releaseGroup{group("rg", c.album, "Album", "2020-01-01")}
			stored := libraryTrack{ID: "t", Title: c.stored, Artist: c.artist, Album: c.album, ISRC: c.isrc, Year: 2020}
			got := deriveSong(t, rec, homes, deezerCrediting(c.isrc, c.stored, c.artist, c.album), stored)
			if _, corrected := got["title_correct"]; corrected {
				t.Fatalf("title corrected to musicbrainz's spelling: %+v", got["title_correct"])
			}
			if d := got["title_differs"]; d.Fix != nil || !strings.Contains(d.Detail, reasonTitleUncorroborated) {
				t.Fatalf("got %+v", got)
			}
		})
	}
}

func TestDeriveAll_ATitleDeezerCarriesToIsCorrected(t *testing.T) {
	rec := recording{ID: "rec", Title: "Ángel", Artist: "Jon Secada", Primary: "Jon Secada", ISRCs: []string{"USEM39200001"}}
	stored := libraryTrack{ID: "t", Title: "Angel", Artist: "Jon Secada", Album: "Jon Secada", ISRC: "USEM39200001", Year: 1992}
	got := deriveSong(t, rec, []releaseGroup{group("rg", "Jon Secada", "Album", "1992-01-01")}, deezerCrediting("USEM39200001", "Ángel", "Jon Secada", "Jon Secada"), stored)
	if fix := got["title_correct"].Fix; fix == nil || *fix.NewValue != "Ángel" {
		t.Fatalf("got %+v", got)
	}
}

func TestDeriveAll_AnArtistChangeNeedsDeezerToCreditTheNewArtist(t *testing.T) {
	const isrc = "USUM72100002"
	rec := recording{ID: "rec", Title: "556 (Green Tip)", Artist: "iayze", Primary: "iayze", ISRCs: []string{isrc}}
	homes := []releaseGroup{group("rg", "Pop Out", "Album", "2024-01-01")}
	stored := libraryTrack{ID: "t", Title: "556 (Green Tip)", Artist: "Jace!", Album: "Pop Out", ISRC: isrc, Year: 2024}
	got := deriveSong(t, rec, homes, deezerCrediting(isrc, "556 (Green Tip)", "Jace!", "Pop Out"), stored)
	if _, corrected := got["artist_correct"]; corrected {
		t.Fatalf("artist leaked: %+v", got["artist_correct"])
	}
	if d := got["artist_differs"]; d.Fix != nil || !strings.Contains(d.Detail, "the catalogs disagree on the artist") {
		t.Fatalf("got %+v", got)
	}
}

func TestDeriveAll_BrunoMarsBecomesSilkSonicWhenDeezerCreditsSilkSonic(t *testing.T) {
	const isrc = "USAT22000001"
	rec := recording{ID: "rec", Title: "Leave the Door Open", Artist: "Silk Sonic", Primary: "Silk Sonic", ISRCs: []string{isrc}}
	homes := []releaseGroup{group("rg", "An Evening With Silk Sonic", "Album", "2021-11-12")}
	stored := libraryTrack{ID: "t", Title: "Leave the Door Open", Artist: "Bruno Mars", Album: "An Evening With Silk Sonic", ISRC: isrc, Year: 2021}
	applied := deriveSong(t, rec, homes, deezerCrediting(isrc, "Leave the Door Open", "Silk Sonic", "An Evening With Silk Sonic"), stored)
	if fix := applied["artist_correct"].Fix; fix == nil || *fix.NewValue != "Silk Sonic" {
		t.Fatalf("got %+v", applied)
	}
	held := deriveSong(t, rec, homes, deezerCrediting(isrc, "Leave the Door Open", "Bruno Mars", "An Evening With Silk Sonic"), stored)
	if held["artist_differs"].Fix != nil || held["artist_correct"].Fix != nil {
		t.Fatalf("got %+v", held)
	}
}

func TestDeriveAll_AnEstateSaleTrackStaysOnItsAlbumGroupInsteadOfMovingToTheBaseAlbum(t *testing.T) {
	const isrc = "USSM12100001"
	rec := recording{ID: "rec", Title: "WHAT A DAY", Artist: "Tyler, The Creator", Primary: "Tyler, The Creator", ISRCs: []string{isrc}}
	homes := []releaseGroup{group("rg", "CALL ME IF YOU GET LOST", "Album", "2021-06-25")}
	stored := libraryTrack{
		ID: "t", Title: "WHAT A DAY", Artist: "Tyler, The Creator", Album: "CALL ME IF YOU GET LOST: The Estate Sale", ISRC: isrc, Year: 2021, TrackNumber: 22,
	}
	got := deriveSong(t, rec, homes, &fakeDeezer{}, stored)
	if _, moved := got["album_correct"]; moved {
		t.Fatalf("album moved: %+v", got["album_correct"])
	}
	if _, lost := got["track_number_correct"]; lost {
		t.Fatalf("track number rewritten: %+v", got["track_number_correct"])
	}
}

func TestDeriveAll_AnAlbumWhoseDeezerTracklistLacksTheRecordingIsNeverTheCorrection(t *testing.T) {
	dz := celebrationDeezer()
	album := dz.albums["80"]
	album.Tracks = []deezerAlbumTrack{{ID: "9000", Title: "Celebration (Part 2)"}}
	dz.albums["80"] = album
	got := byCategory(newDeriver(celebrationMusicBrainz(), dz).deriveAll(t.Context(), []boundTrack{celebrationTrack()}).Findings)
	if _, moved := got["album_correct"]; moved {
		t.Fatalf("album moved: %+v", got["album_correct"])
	}
	if d := got["album_differs"]; d.Fix != nil || !strings.Contains(d.Detail, reasonAlbumLacksTrack) {
		t.Fatalf("got %+v", got)
	}
}

func TestDeriveAll_AStoredOriginalAlbumIsNotReplacedByALaterReleaseOfTheRecording(t *testing.T) {
	const isrc = "USDJ21500001"
	rec := recording{ID: "rec", Title: "oui", Artist: "Jeremih", Primary: "Jeremih", ISRCs: []string{isrc}}
	lateNights := group("rg-late-nights", "Late Nights: The Album", "Album", "2015-12-04", "Mixtape/Street")
	lateNights.Artist, lateNights.ArtistNames = "Jeremih", []string{"Jeremih"}
	homes := []releaseGroup{lateNights, group("rg-say-oui", "Jeremih: Say Oui", "Album", "2026-02-13")}
	stored := libraryTrack{ID: "t", Title: "oui", Artist: "Jeremih", Album: "Late Nights: The Album", ISRC: isrc, Year: 2015}
	got := deriveSong(t, rec, homes, &fakeDeezer{}, stored)
	for _, kind := range []category{"album_correct", "year_correct"} {
		if _, corrected := got[kind]; corrected {
			t.Errorf("%s: %+v", kind, got[kind])
		}
	}
	if d := got["album_differs"]; d.Fix != nil || !strings.Contains(d.Detail, reasonStoredIsOriginal) {
		t.Errorf("album_differs: %+v", d)
	}
	if _, differs := got["year_differs"]; differs {
		t.Errorf("year still differs against the stored album: %+v", got["year_differs"])
	}
}

func TestDeriveAll_AnEstateSaleTrackWithoutANumberIsNotFilledFromTheBaseAlbum(t *testing.T) {
	const isrc = "USSM12100001"
	rec := recording{ID: "rec", Title: "WHAT A DAY", Artist: "Tyler, The Creator", Primary: "Tyler, The Creator", ISRCs: []string{isrc}}
	homes := []releaseGroup{group("rg", "CALL ME IF YOU GET LOST", "Album", "2021-06-25")}
	dz := deezerCrediting(isrc, "WHAT A DAY", "Tyler, The Creator", "CALL ME IF YOU GET LOST")
	stored := libraryTrack{ID: "t", Title: "WHAT A DAY", Artist: "Tyler, The Creator", Album: "CALL ME IF YOU GET LOST: The Estate Sale", ISRC: isrc, Year: 2021}
	got := deriveSong(t, rec, homes, dz, stored)
	for _, kind := range []category{"track_number_fill", "genre_fill", "artwork_url_fill"} {
		if _, filled := got[kind]; filled {
			t.Errorf("%s from the base album: %+v", kind, got[kind])
		}
	}
	if d := got["track_number_differs"]; d.Fix != nil || !strings.Contains(d.Detail, "another edition") {
		t.Fatalf("got %+v", got)
	}
}

func TestDeriveAll_AHeldAlbumFillsTheFieldsFromTheStoredAlbumItself(t *testing.T) {
	const isrc = "USDJ21500001"
	rec := recording{ID: "rec", Title: "oui", Artist: "Jeremih", Primary: "Jeremih", ISRCs: []string{isrc}}
	lateNights := group("rg-late-nights", "Late Nights: The Album", "Album", "2015-12-04", "Mixtape/Street")
	lateNights.Artist, lateNights.ArtistNames = "Jeremih", []string{"Jeremih"}
	homes := []releaseGroup{lateNights, group("rg-say-oui", "Jeremih: Say Oui", "Album", "2026-02-13")}
	dz := deezerCrediting(isrc, "oui", "Jeremih", "Jeremih: Say Oui")
	stored := libraryTrack{ID: "t", Title: "oui", Artist: "Jeremih", Album: "Late Nights: The Album", ISRC: isrc}
	got := deriveSong(t, rec, homes, dz, stored)
	y := got["year_fill"]
	if y.Fix == nil || !strings.Contains(y.Detail, `"2015"`) {
		t.Errorf("year not filled from Late Nights' own release group (2015-12-04): %+v", y)
	}
	if d := got["album_differs"]; d.Fix != nil || !strings.Contains(d.Detail, reasonStoredIsOriginal) {
		t.Fatalf("got %+v", got)
	}
}

func TestDeriveAll_AStoredAlbumTitleSharedByTwoGroupsAdoptsOnlyTheLeadArtistsOwnAlbum(t *testing.T) {
	const isrc = "USDJ21500001"
	rec := recording{ID: "rec", Title: "oui", Artist: "Jeremih", Primary: "Jeremih", ISRCs: []string{isrc}}
	compilation := group("rg-va", "Late Nights: The Album", "Album", "1999-01-01", "Compilation")
	compilation.Artist, compilation.ArtistNames = "Various Artists", []string{"Various Artists"}
	own := group("rg-own", "Late Nights: The Album", "Album", "2015-12-04", "Mixtape/Street")
	own.Artist, own.ArtistNames = "Jeremih", []string{"Jeremih"}
	later := group("rg-say-oui", "Jeremih: Say Oui", "Album", "2026-02-13")
	stored := libraryTrack{ID: "t", Title: "oui", Artist: "Jeremih", Album: "Late Nights: The Album", ISRC: isrc}
	orders := map[string][]releaseGroup{
		"compilation first":  {compilation, own, later},
		"artist album first": {own, compilation, later},
	}
	for name, homes := range orders {
		got := deriveSong(t, rec, homes, deezerCrediting(isrc, "oui", "Jeremih", "Jeremih: Say Oui"), stored)
		if y := got["year_fill"]; y.Fix == nil || !strings.Contains(y.Detail, `"2015"`) {
			t.Errorf("%s: year not from the artist's own album: %+v", name, y)
		}
		if aa := got["album_artist_differs"]; aa.Fix != nil && strings.Contains(aa.Detail, "Various") {
			t.Errorf("%s: adopted Various Artists: %+v", name, aa)
		}
	}
}

func TestDeriveAll_TwoSameTitledGroupsOfTheLeadArtistFillNothingFromEither(t *testing.T) {
	const isrc = "USDJ21500004"
	rec := recording{ID: "rec", Title: "Buddy Holly", Artist: "Weezer", Primary: "Weezer", ISRCs: []string{isrc}}
	blue := group("rg-blue", "Weezer", "Album", "1994-05-10", "Mixtape/Street")
	green := group("rg-green", "Weezer", "Album", "2001-05-15", "Mixtape/Street")
	for _, g := range []*releaseGroup{&blue, &green} {
		g.Artist, g.ArtistNames = "Weezer", []string{"Weezer"}
	}
	chosen := group("rg-chosen", "Make Believe", "Album", "2030-01-01")
	stored := libraryTrack{ID: "t", Title: "Buddy Holly", Artist: "Weezer", Album: "Weezer", ISRC: isrc}
	orders := map[string][]releaseGroup{"blue first": {blue, green, chosen}, "green first": {green, blue, chosen}}
	for name, homes := range orders {
		got := deriveSong(t, rec, homes, deezerCrediting(isrc, "Buddy Holly", "Weezer", "Make Believe"), stored)
		y := got["year_fill"]
		if strings.Contains(y.Detail, `"1994"`) || strings.Contains(y.Detail, `"2001"`) {
			t.Errorf("%s: adopted one of two same-titled albums: %+v", name, y)
		}
		if d := got["album_differs"]; d.Fix != nil {
			t.Errorf("%s: album moved: %+v", name, d)
		}
	}
}

type replayExpectation struct {
	applies  map[string]string
	keeps    []string
	reported []string
	settles  []string
}

type realProdCase struct {
	name         string
	fixture      string
	recording    string
	blankCover   bool
	stored       libraryTrack
	expectations replayExpectation
}

type recordedResponse struct {
	Status int             `json:"status"`
	Body   json.RawMessage `json:"body"`
}

func replayCatalogs(t *testing.T, name string) (musicBrainz, deezer) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "replay", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var recorded map[string]recordedResponse
	if err := json.Unmarshal(raw, &recorded); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		answer, ok := recorded[r.URL.RequestURI()]
		if !ok {
			t.Errorf("request with no recorded answer: %s", r.URL.RequestURI())
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(answer.Status)
		_, _ = w.Write(answer.Body)
	}))
	t.Cleanup(srv.Close)
	return fastMusicBrainz(srv), fastDeezer(srv)
}

func appliedFixes(found []finding) map[string]string {
	applied := map[string]string{}
	for _, f := range found {
		if f.Fix != nil && f.Fix.NewValue != nil {
			applied[f.Fix.Field] = *f.Fix.NewValue
		}
	}
	return applied
}

func reportedFields(found []finding) []string {
	var fields []string
	for _, f := range found {
		if field, isReport := strings.CutSuffix(string(f.Category), "_differs"); isReport {
			fields = append(fields, field)
		}
	}
	return fields
}

func dzCover(md5, size string) string {
	return "https://cdn-images.dzcdn.net/images/cover/" + md5 + "/" + size + "-000000-80-0-0.jpg"
}

func TestDeriveAll_RealProdCasesKeepTheirOutcomePerField(t *testing.T) {
	realProdCases := []realProdCase{
		{
			name: "ken-carson-yale",
			stored: libraryTrack{
				ID: "t", Title: "Yale", Artist: "Ken Carson", Album: "Teen X", AlbumArtist: "Ken Carson", ISRC: "TCAFB2005896", Year: 2020, Genre: "Rap/Hip Hop",
				TrackNumber: 2, ArtworkURL: dzCover("1c0d7ab95997d32f951bd0c2d6e1f8c1", "1000x1000"), FileSeconds: 106,
			},
			expectations: replayExpectation{keeps: []string{"artist", "album_artist", "album"}},
		},
		{
			name: "travis-scott-dont-play",
			stored: libraryTrack{
				ID: "t", Title: "Don't Play (feat. The 1975 & Big Sean)", Artist: "Travis Scott", Album: "DAYS BEFORE RODEO", AlbumArtist: "Travis Scott", ISRC: "USSM11405561",
				Year: 2014, Genre: "Rap/Hip Hop", TrackNumber: 1, ArtworkURL: dzCover("2ee938c5dbac408d30dbe998eb6b1280", "1000x1000"), FileSeconds: 285,
			},
			expectations: replayExpectation{keeps: []string{"artist", "album_artist", "title"}},
		},
		{
			name: "tyler-what-a-day",
			stored: libraryTrack{
				ID: "t", Title: "WHAT A DAY", Artist: "Tyler, The Creator", Album: "CALL ME IF YOU GET LOST: The Estate Sale", AlbumArtist: "Tyler, The Creator",
				ISRC: "USQX92301935", Year: 2023, Genre: "Rap/Hip Hop", TrackNumber: 19, ArtworkURL: dzCover("40b4545c7f373fb519dfcc9092c2ccf6", "1000x1000"), FileSeconds: 216,
			},
			expectations: replayExpectation{keeps: []string{"album"}, settles: []string{"track_number", "genre", "artwork_url"}},
		},
		{
			name: "jeremih-oui",
			stored: libraryTrack{
				ID: "t", Title: "oui", Artist: "Jeremih", Album: "Late Nights: The Album", AlbumArtist: "Jeremih", ISRC: "USUM71513397", Year: 2015, Genre: "R&B",
				TrackNumber: 4, ArtworkURL: dzCover("56907774d655c7924a309edca4351a76", "1000x1000"), FileSeconds: 238,
			},
			expectations: replayExpectation{keeps: []string{"album", "year", "track_number", "artwork_url"}},
		},
		{
			name:      "jeremih-oui-original-recording",
			recording: "0d751dff-30e6-46fd-bc75-574f973d9eac",
			stored: libraryTrack{
				ID: "t", Title: "oui", Artist: "Jeremih", Album: "Late Nights: The Album", AlbumArtist: "Jeremih", ISRC: "USUM71513397", Year: 2015, Genre: "R&B",
				TrackNumber: 4, ArtworkURL: dzCover("56907774d655c7924a309edca4351a76", "1000x1000"), FileSeconds: 238,
			},
			expectations: replayExpectation{keeps: []string{"album", "year", "track_number", "artwork_url"}},
		},
		{
			name: "blackpink-jump",
			stored: libraryTrack{
				ID: "t", Title: "JUMP", Artist: "BLACKPINK", Album: "JUMP", ISRC: "KRA402400094", Genre: "K-pop", ArtworkURL: dzCover("6ef57c5bebc2c8ff9710cf3eca0addb0", "500x500"), FileSeconds: 165,
			},
			expectations: replayExpectation{keeps: []string{"title"}, reported: []string{"title"}},
		},
		{
			name: "jon-secada-angel",
			stored: libraryTrack{
				ID: "t", Title: "Angel", Artist: "Jon Secada", Album: "Jon Secada", AlbumArtist: "Jon Secada", ISRC: "USSB29200104",
				ArtworkURL: dzCover("a6f22e152a4a4f2c659b57b4b3789ce5", "500x500"), FileSeconds: 249,
			},
			expectations: replayExpectation{keeps: []string{"title"}, reported: []string{"title"}},
		},
		{
			name: "cordae-c-carter",
			stored: libraryTrack{
				ID: "t", Title: "C Carter", Artist: "Cordae", Album: "From a Birds Eye View", AlbumArtist: "Cordae", ISRC: "USAT22107997", Year: 2022, Genre: "Rap/Hip Hop",
				TrackNumber: 8, ArtworkURL: dzCover("a4e5621aeba74613f9c66368359f3be2", "1000x1000"), FileSeconds: 193,
			},
			expectations: replayExpectation{keeps: []string{"title"}, reported: []string{"title"}},
		},
		{
			name: "jace-556-green-tip",
			stored: libraryTrack{
				ID: "t", Title: "556 (Green Tip)", Artist: "Jace!", Album: "556 (Green Tip)", AlbumArtist: "Jace!", ISRC: "USLD91736763", Year: 2022, Genre: "Rap/Hip Hop",
				TrackNumber: 5, ArtworkURL: dzCover("2a633a006eda5e61224455eff786f199", "1000x1000"), FileSeconds: 117,
			},
			expectations: replayExpectation{keeps: []string{"artist", "album_artist"}},
		},
		{
			name: "bruno-mars-skate",
			stored: libraryTrack{
				ID: "t", Title: "Skate", Artist: "Bruno Mars", Album: "An Evening With Silk Sonic", AlbumArtist: "Bruno Mars", ISRC: "USAT22104222", Year: 2021, Genre: "Pop",
				TrackNumber: 8, ArtworkURL: dzCover("faada8a8c545b2e3d454267b55c82e14", "1000x1000"), FileSeconds: 203,
			},
			expectations: replayExpectation{
				keeps:    []string{"artist", "album_artist"},
				reported: []string{"artist", "album_artist"},
			},
		},
		{
			name: "pitbull-give-me-everything",
			stored: libraryTrack{
				ID: "t", Title: "Give Me Everything (feat. Nayer)", Artist: "Pitbull", Album: "Greatest Hits", AlbumArtist: "Pitbull", ISRC: "USJAY1100032",
				ArtworkURL: dzCover("bafa7580e18f2d795792b09b2d2bed43", "500x500"), FileSeconds: 252.336,
			},
			expectations: replayExpectation{applies: map[string]string{"album": "Planet Pit", "year": "2011", "track_number": "2"}, keeps: []string{"artist"}},
		},
		{
			name: "che-tuesday",
			stored: libraryTrack{
				ID: "t", Title: "Tuesday", Artist: "che", Album: "Empty Clip", AlbumArtist: "Che", ISRC: "CH7812677039",
				ArtworkURL: dzCover("68a7ee84662ebcde06b430ca59bb99ef", "500x500"), FileSeconds: 170,
			},
			expectations: replayExpectation{applies: map[string]string{
				"title": "2sday", "artwork_url": dzCover("fa61c7182566f93516a4a83f73187549", "1000x1000"),
			}},
		},
		{
			name:       "kanye-stronger",
			blankCover: true,
			stored: libraryTrack{
				ID: "t", Title: "Stronger", Artist: "Kanye West", Album: "Graduation", AlbumArtist: "Kanye West", ISRC: "USUM70741299", Year: 2013, Genre: "rap", TrackNumber: 1,
				ArtworkURL: dzCover("d167d9a9b2f3f49ed72878a00530415b", "1000x1000"), FileSeconds: 311,
			},
			expectations: replayExpectation{applies: map[string]string{
				"artwork_url": "https://coverartarchive.org/release/06a81817-093d-40f0-aef2-90673fa550ae/front", "year": "2007",
			}},
		},
		{
			name:       "kanye-stronger-album-artist",
			blankCover: true,
			stored: libraryTrack{
				ID: "t", Title: "Stronger", Artist: "Kanye West", Album: "Graduation", AlbumArtist: "Kanye West", ISRC: "USUM70741299", Year: 2013, Genre: "rap", TrackNumber: 1,
				ArtworkURL: dzCover("d167d9a9b2f3f49ed72878a00530415b", "1000x1000"), FileSeconds: 311,
			},
			expectations: replayExpectation{keeps: []string{"album_artist"}, reported: []string{"album_artist"}},
		},
		{
			name:      "rihanna-we-found-love",
			recording: "70b75f40-442e-48b2-a41e-6b7490b73fbe",
			stored: libraryTrack{
				ID: "t", Title: "We Found Love (Album Version)", Artist: "Rihanna", Album: "Talk That Talk (Deluxe)", ISRC: "USUM71115507", Genre: "Pop",
				ArtworkURL: dzCover("5199f89d5113a83b5086463d5d0c9415", "1000x1000"), FileSeconds: 216,
			},
			expectations: replayExpectation{applies: map[string]string{"album_artist": "Rihanna", "year": "2011"}, keeps: []string{"album"}, reported: []string{"album"}},
		},
		{
			name:      "u2-still-havent-found",
			recording: "d50715ed-96a3-4de8-9b19-cb58667c879f",
			stored: libraryTrack{
				ID: "t", Title: "I Still Haven’t Found What I’m Looking For", Artist: "U2", Album: "Greatest Hits", ISRC: "GBUM70709783",
				ArtworkURL: "https://coverartarchive.org/release-group/e7044c49-02de-3130-b6df-a1a8c403644a/front-500", FileSeconds: 275.69,
			},
			expectations: replayExpectation{keeps: []string{"album", "album_artist", "year"}, reported: []string{"album"}},
		},
		{
			name:      "tozzi-ti-amo",
			recording: "eb761497-ec38-4023-b280-faeb7f0eef74",
			stored: libraryTrack{
				ID: "t", Title: "Ti Amo", Artist: "Umberto Tozzi", Album: "Ti amo", AlbumArtist: "Umberto Tozzi", ISRC: "ITR007700021", Year: 2021, Genre: "Pop", TrackNumber: 1,
				ArtworkURL: dzCover("1dde50527d5cf2167dc5a3f01c8df6f3", "500x500"), FileSeconds: 246,
			},
			expectations: replayExpectation{keeps: []string{"album", "album_artist", "year"}, reported: []string{"album"}},
		},
		{
			name:      "usher-yeah",
			recording: "4f595ab5-18bb-4007-b17d-066a0e704234",
			stored: libraryTrack{
				ID: "t", Title: "Yeah! (feat. Lil Jon & Ludacris)", Artist: "USHER", Album: "Confessions (Expanded Edition)", ISRC: "USAR10301423",
				ArtworkURL: dzCover("b89c20012cccb051c8a4e04d98386f95", "500x500"), FileSeconds: 251,
			},
			expectations: replayExpectation{applies: map[string]string{"album_artist": "USHER", "year": "2004"}, keeps: []string{"album"}, reported: []string{"album"}},
		},
		{
			name: "sade-smooth-operator",
			stored: libraryTrack{
				ID: "t", Title: "Smooth Operator (2011 Remastered)", Artist: "Sade", Album: "The Ultimate Collection", AlbumArtist: "Sade", ISRC: "GBARL1100319", Year: 2011,
				Genre: "R&B", TrackNumber: 2, ArtworkURL: dzCover("e2fd836cedf9336dc5c725dc3c4df5e0", "500x500"), FileSeconds: 258.7,
			},
			expectations: replayExpectation{keeps: []string{"album", "album_artist", "year"}, reported: []string{"album"}},
		},
		{
			name: "spandau-ballet-gold",
			stored: libraryTrack{
				ID: "t", Title: "Gold", Artist: "Spandau Ballet", Album: "40 Years · The Greatest Hits", ISRC: "GBAYK8300018",
				ArtworkURL: "https://coverartarchive.org/release-group/85157d23-8324-491d-8afd-f2ce84f794cd/front-500", FileSeconds: 231.49,
			},
			expectations: replayExpectation{keeps: []string{"album", "album_artist", "year"}, reported: []string{"album"}},
		},
		{
			name: "calvin-harris-one-kiss",
			stored: libraryTrack{
				ID: "t", Title: "One Kiss", Artist: "Calvin Harris", Album: "One Kiss", AlbumArtist: "Calvin Harris", ISRC: "GBARL1800368", Year: 2018, TrackNumber: 1,
				ArtworkURL: dzCover("0397baea24f861db7ee63fb1c70391f9", "500x500"), FileSeconds: 214.86,
			},
			expectations: replayExpectation{
				applies:  map[string]string{"album_artist": "Calvin Harris & Dua Lipa", "artwork_url": "https://coverartarchive.org/release/c80adc92-c63c-4f3e-8bb8-f9a3992631dc/front"},
				keeps:    []string{"album", "year", "track_number"},
				reported: []string{"album"},
			},
		},
		{
			name: "coldplay-something-just-like-this",
			stored: libraryTrack{
				ID: "t", Title: "Something Just Like This", Artist: "Coldplay", Album: "Something Just Like This", AlbumArtist: "Coldplay", ISRC: "USQX91700278", Year: 2017,
				TrackNumber: 1, ArtworkURL: dzCover("2ae6c01a51296a7ec6d89c96a4fac32c", "500x500"), FileSeconds: 247.6,
			},
			expectations: replayExpectation{
				applies:  map[string]string{"artwork_url": "https://cdn-images.dzcdn.net/images/cover/2ae6c01a51296a7ec6d89c96a4fac32c/1000x1000-000000-80-0-0.jpg"},
				keeps:    []string{"album", "year"},
				reported: []string{"album"},
			},
		},
		{
			name:      "gunna-alright",
			recording: "4a8c7f79-1c4f-477d-a6d9-5ecf1d15be39",
			stored: libraryTrack{
				ID: "t", Title: "alright", Artist: "Gunna", Album: "a Gift & a Curse", AlbumArtist: "Gunna", ISRC: "USAT22306583", Year: 2023, Genre: "Rap/Hip Hop", TrackNumber: 15,
				ArtworkURL: dzCover("35446b14e181f0a3fe415c06fec08d0b", "1000x1000"), FileSeconds: 184.03,
			},
			expectations: replayExpectation{keeps: []string{"album", "track_number", "artwork_url"}, reported: []string{"album"}},
		},
		{
			name: "homixide-gang-ssn",
			stored: libraryTrack{
				ID: "t", Title: "SSN", Artist: "Homixide Gang", Album: "SSN", AlbumArtist: "Homixide Gang", ISRC: "QZNWT2118702", Year: 2021, Genre: "Rap/Hip Hop", TrackNumber: 1,
				ArtworkURL: dzCover("ee11ae9a62b81ab5fda53b88cd805094", "1000x1000"), FileSeconds: 101.4,
			},
			expectations: replayExpectation{keeps: []string{"artist", "album_artist"}, reported: []string{"album_artist"}},
		},
		{
			name: "luba-everytime",
			stored: libraryTrack{
				ID: "t", Title: "Everytime I See Your Picture", Artist: "Luba", Album: "Secrets & Sins", ISRC: "CAE158400020", Genre: "Pop", TrackNumber: 4,
				ArtworkURL: dzCover("887a27e6b666f6024da92564a52a23a6", "1000x1000"), FileSeconds: 240.59,
			},
			expectations: replayExpectation{applies: map[string]string{"album_artist": "Luba"}},
		},
		{
			name: "dave-sprinter",
			stored: libraryTrack{
				ID: "t", Title: "Sprinter", Artist: "Dave", Album: "Sprinter", AlbumArtist: "Dave & Central Cee", ISRC: "GBUM72305159", Year: 2023, Genre: "Pop", TrackNumber: 2,
				ArtworkURL: dzCover("f97888ccd13c3be72765e110a0797d37", "1000x1000"), FileSeconds: 229,
			},
			expectations: replayExpectation{applies: map[string]string{"track_number": "1"}, reported: []string{"genre"}},
		},
		{
			name: "jack-harlow-churchill-downs",
			stored: libraryTrack{
				ID: "t", Title: "Churchill Downs (feat. Drake)", Artist: "Jack Harlow", Album: "Come Home The Kids Miss You", AlbumArtist: "Jack Harlow", ISRC: "USAT22203487", Year: 2022,
				Genre: "Rap/Hip Hop", TrackNumber: 24, ArtworkURL: dzCover("b66494c3e751cdaa1ce4a14ed6125f15", "1000x1000"), FileSeconds: 309,
			},
			expectations: replayExpectation{applies: map[string]string{"track_number": "10"}, keeps: []string{"album", "year"}, reported: []string{"genre"}},
		},
		{
			name: "don-toliver-excavator",
			stored: libraryTrack{
				ID: "t", Title: "Excavator", Artist: "Don Toliver", Album: "OCTANE", AlbumArtist: "Don Toliver", ISRC: "USAT22600128", Year: 2026, Genre: "Rap/Hip Hop", TrackNumber: 13,
				ArtworkURL: dzCover("123eb0268dfea84370a28c4a2114dc28", "1000x1000"), FileSeconds: 211,
			},
			expectations: replayExpectation{keeps: []string{"album", "track_number"}, reported: []string{"genre"}},
		},
		{
			name:    "held-album-tyler-what-a-day-estate-sale",
			fixture: "tyler-what-a-day",
			stored: libraryTrack{
				ID: "t", Title: "WHAT A DAY", Artist: "Tyler, The Creator", Album: "CALL ME IF YOU GET LOST: The Estate Sale", AlbumArtist: "Tyler, The Creator",
				ISRC: "USQX92301935", Year: 2023, Genre: "Rap/Hip Hop", TrackNumber: 19, ArtworkURL: dzCover("40b4545c7f373fb519dfcc9092c2ccf6", "1000x1000"), FileSeconds: 216,
			},
			expectations: replayExpectation{settles: []string{"album", "year", "track_number", "genre", "artwork_url"}},
		},
		{
			name:      "held-album-jeremih-oui-late-nights",
			fixture:   "jeremih-oui-original-recording",
			recording: "0d751dff-30e6-46fd-bc75-574f973d9eac",
			stored: libraryTrack{
				ID: "t", Title: "oui", Artist: "Jeremih", Album: "Late Nights: The Album", AlbumArtist: "Jeremih", ISRC: "USUM71513397", Year: 2015, Genre: "R&B",
				TrackNumber: 4, ArtworkURL: dzCover("56907774d655c7924a309edca4351a76", "1000x1000"), FileSeconds: 238,
			},
			expectations: replayExpectation{settles: []string{"year", "track_number", "genre", "artwork_url"}},
		},
		{
			name: "held-album-daniel-caesar-valentina-bonus-version",
			stored: libraryTrack{
				ID: "t", Title: "Valentina", Artist: "Daniel Caesar", Album: "NEVER ENOUGH (Bonus Version)", AlbumArtist: "Daniel Caesar", ISRC: "USUG12209353", Year: 2023,
				Genre: "R&B", TrackNumber: 18, ArtworkURL: dzCover("0d571082af7c78114321031d7f84d331", "1000x1000"), FileSeconds: 154,
			},
			expectations: replayExpectation{applies: map[string]string{"track_number": "2"}, settles: []string{"year", "genre", "artwork_url"}},
		},
	}
	for _, c := range realProdCases {
		t.Run(c.name, func(t *testing.T) {
			mb, dz := replayCatalogs(t, cmp.Or(c.fixture, c.name))
			d := newDeriver(mb, dz)
			d.covers = fakeCoverProbe{placeholders: map[string]bool{}}
			if c.blankCover {
				d.covers = fakeCoverProbe{placeholders: map[string]bool{c.stored.ArtworkURL: true}}
			}
			bound := boundTrack{libraryTrack: c.stored, IdentityISRC: c.stored.ISRC, MBRecordingID: c.recording}
			got := d.deriveAll(t.Context(), []boundTrack{bound})
			if got.Aborted != nil {
				t.Fatalf("aborted: %v", got.Aborted)
			}
			applied := appliedFixes(got.Findings)
			for field, want := range c.expectations.applies {
				if applied[field] != want {
					t.Errorf("%s: applied %q, want %q (all applied: %v)", field, applied[field], want, applied)
				}
			}
			for _, field := range c.expectations.keeps {
				if value, changed := applied[field]; changed {
					t.Errorf("%s was changed to %q", field, value)
				}
			}
			for _, field := range c.expectations.settles {
				if value, changed := applied[field]; changed {
					t.Errorf("%s was changed to %q", field, value)
				}
				if slices.Contains(reportedFields(got.Findings), field) {
					t.Errorf("%s is still reported (reported: %v)", field, reportedFields(got.Findings))
				}
			}
			for _, field := range c.expectations.reported {
				if !slices.Contains(reportedFields(got.Findings), field) {
					t.Errorf("%s is not reported (reported: %v)", field, reportedFields(got.Findings))
				}
			}
		})
	}
}

func ouiRecording(isrc string) recording {
	return recording{ID: "rec", Title: "oui", Artist: "Jeremih", Primary: "Jeremih", ISRCs: []string{isrc}}
}

func TestDeriveAll_JeremihOuiStaysOnLateNightsWhenDeezerListsItOnTheEarlierStoredAlbum(t *testing.T) {
	const isrc = "USDJ21500001"
	homes := []releaseGroup{
		group("rg-oui", "Oui", "Single", "2015-10-09"),
		group("rg-say-oui", "Jeremih: Say Oui", "Album", "2026-08-14"),
	}
	dz := deezerCrediting(isrc, "oui", "Jeremih", "Jeremih: Say Oui")
	dz.search = map[string][]deezerAlbum{"Late Nights: The Album": {{ID: "20", Title: "Late Nights: The Album"}}}
	dz.albums["20"] = deezerAlbum{ID: "20", Title: "Late Nights: The Album", ReleaseDate: "2015-12-04", Tracks: []deezerAlbumTrack{{ID: "21", Title: "oui"}}}
	dz.byID = map[string]deezerTrack{"21": {ID: "21", ISRC: isrc, Title: "oui", Artist: "Jeremih", Position: 4}}
	stored := libraryTrack{ID: "t", Title: "oui", Artist: "Jeremih", Album: "Late Nights: The Album", ISRC: isrc, Year: 2015, TrackNumber: 4, ArtworkURL: "https://cdn/late.jpg"}
	got := deriveSong(t, ouiRecording(isrc), homes, dz, stored)
	for _, kind := range []category{"album_correct", "year_correct", "track_number_correct", "artwork_url_correct"} {
		if _, moved := got[kind]; moved {
			t.Errorf("%s: %+v", kind, got[kind])
		}
	}
	if d := got["album_differs"]; d.Fix != nil || !strings.Contains(d.Detail, reasonStoredListsTrackEarlier) {
		t.Errorf("album_differs: %+v", d)
	}
	for _, kind := range []category{"year_differs", "track_number_differs"} {
		if _, differs := got[kind]; differs {
			t.Errorf("%s still differs against the stored album: %+v", kind, got[kind])
		}
	}
	if d := got["artwork_url_differs"]; d.Fix != nil || !strings.Contains(d.Detail, "the stored album has no cover") {
		t.Errorf("artwork_url_differs: %+v", d)
	}
}

func TestDeriveAll_Jace556StaysPutWhenTheCatalogsDisagreeOnTheArtist(t *testing.T) {
	const isrc = "USUM72100002"
	rec := recording{ID: "rec", Title: "556 (Green Tip)", Artist: "iayze", Primary: "iayze", ISRCs: []string{isrc}}
	homes := []releaseGroup{group("rg", "Virtuous", "Album", "2022-03-02")}
	stored := libraryTrack{ID: "t", Title: "556 (Green Tip)", Artist: "Jace!", Album: "556 (Green Tip)", ISRC: isrc, Year: 2021}
	got := deriveSong(t, rec, homes, deezerCrediting(isrc, "556 (Green Tip)", "Jace!", "Virtuous"), stored)
	for _, kind := range []category{"album_correct", "year_correct", "artist_correct", "track_number_fill", "genre_fill"} {
		if _, applied := got[kind]; applied {
			t.Errorf("%s: %+v", kind, got[kind])
		}
	}
	if d := got["album_differs"]; d.Fix != nil || !strings.Contains(d.Detail, reasonArtistUncorroborated) {
		t.Fatalf("got %+v", got)
	}
}

func TestDeriveAll_AnEstateSaleTracksYearIsHeldWithItsEdition(t *testing.T) {
	const isrc = "USSM12100001"
	rec := recording{ID: "rec", Title: "WHAT A DAY", Artist: "Tyler, The Creator", Primary: "Tyler, The Creator", ISRCs: []string{isrc}}
	homes := []releaseGroup{group("rg", "CALL ME IF YOU GET LOST", "Album", "2021-06-25")}
	stored := libraryTrack{ID: "t", Title: "WHAT A DAY", Artist: "Tyler, The Creator", Album: "CALL ME IF YOU GET LOST: The Estate Sale", ISRC: isrc, Year: 2023}
	got := deriveSong(t, rec, homes, deezerCrediting(isrc, "WHAT A DAY", "Tyler, The Creator", "CALL ME IF YOU GET LOST"), stored)
	if _, corrected := got["year_correct"]; corrected {
		t.Fatalf("year rewritten: %+v", got["year_correct"])
	}
	if d := got["year_differs"]; d.Fix != nil || !strings.Contains(d.Detail, "another edition") {
		t.Fatalf("got %+v", got)
	}
}

func TestDeriveAll_ATravisScottMixtapeOutranksTheSingleOfTheSameSong(t *testing.T) {
	const isrc = "USSM11400001"
	rec := recording{ID: "rec", Title: "Don't Play", Artist: "Travis Scott feat. The 1975 & Big Sean", Primary: "Travis Scott", ISRCs: []string{isrc}}
	homes := []releaseGroup{
		group("rg-dbr", "DAYS BEFORE RODEO", "Album", "2014-08-18", "Mixtape/Street"),
		group("rg-single", "Don't Play", "Single", "2021-01-01"),
	}
	stored := libraryTrack{ID: "t", Title: "Don't Play", Artist: "Travis Scott", Album: "DAYS BEFORE RODEO", ISRC: isrc, Year: 2014}
	got := deriveSong(t, rec, homes, &fakeDeezer{}, stored)
	if _, moved := got["album_correct"]; moved {
		t.Fatalf("album moved: %+v", got["album_correct"])
	}
	if !strings.Contains(got["release_chosen"].Detail, "DAYS BEFORE RODEO") {
		t.Fatalf("got %+v", got["release_chosen"])
	}
}
