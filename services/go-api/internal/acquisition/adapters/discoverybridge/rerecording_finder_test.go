package discoverybridge

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	acqports "altune/go-api/internal/acquisition/ports"
	discoveryports "altune/go-api/internal/discovery/ports"
)

const (
	bryanAdams        = "4dbf5678-7a31-406a-abbe-232f8ac2cd63"
	itsOnlyLove       = "7933a65c-2c66-4ec1-9880-e46b5d2f05de"
	itsOnlyLoveClassi = "25d68187-ee71-4f06-9e58-0b49d66bf8a8"
	otherArtist       = "00000000-0000-0000-0000-0000000000aa"
)

type stubRerecordingAuthority struct {
	work        []discoveryports.MBWorkRecording
	details     map[string]discoveryports.MBRecordingDetail
	workErr     error
	detailErr   map[string]error
	detailCalls int
}

func (s *stubRerecordingAuthority) WorkRecordings(context.Context, string) ([]discoveryports.MBWorkRecording, error) {
	return s.work, s.workErr
}

func (s *stubRerecordingAuthority) RecordingDetail(_ context.Context, mbid string) (discoveryports.MBRecordingDetail, bool, error) {
	s.detailCalls++
	if err := s.detailErr[mbid]; err != nil {
		return discoveryports.MBRecordingDetail{}, false, err
	}
	detail, ok := s.details[mbid]
	return detail, ok, nil
}

func official(title, date string) []discoveryports.MBRelease {
	return []discoveryports.MBRelease{{Title: title, Date: date, Status: "Official"}}
}

func itsOnlyLoveOriginal() discoveryports.MBRecordingDetail {
	return discoveryports.MBRecordingDetail{
		MBID: itsOnlyLove, Title: "It’s Only Love", ArtistIDs: []string{bryanAdams},
		Duration: 195, FirstReleaseDate: "1984-10-29", Releases: official("Reckless", "1984-10-29"),
	}
}

func itsOnlyLoveWork() *stubRerecordingAuthority {
	work := []discoveryports.MBWorkRecording{
		{MBID: itsOnlyLove, Title: "It’s Only Love", ArtistIDs: []string{bryanAdams}, Duration: 195},
		{MBID: "e5cdd7d1-727a-4a5d-9318-11a1cb8e19dc", Title: "It’s Only Love (classic version)", ArtistIDs: []string{bryanAdams}},
		{MBID: "b9eec01c-7c0b-48c2-ba44-f0b9952e6599", Title: "It’s Only Love (live at The Royal Albert Hall 2024)", ArtistIDs: []string{bryanAdams}, Duration: 291.986},
		{MBID: "8d393956-4a15-4bf8-982b-d4e0aea20550", Title: "It’s Only Love", ArtistIDs: []string{bryanAdams}, Live: true},
		{MBID: "5d216325", Title: "It's Only Love", ArtistIDs: []string{bryanAdams}, Duration: 302},
		{MBID: "9857887e-aa9a-47b0-868a-0f51049cdaed", Title: "It’s Only Love (classic version)", ArtistIDs: []string{bryanAdams}},
		{MBID: itsOnlyLoveClassi, Title: "It’s Only Love (classic version)", ArtistIDs: []string{bryanAdams}, Duration: 194},
	}
	details := map[string]discoveryports.MBRecordingDetail{itsOnlyLove: itsOnlyLoveOriginal()}
	details["e5cdd7d1-727a-4a5d-9318-11a1cb8e19dc"] = discoveryports.MBRecordingDetail{
		MBID: "e5cdd7d1-727a-4a5d-9318-11a1cb8e19dc", Title: "It’s Only Love (classic version)", ArtistIDs: []string{bryanAdams},
		FirstReleaseDate: "2025-05-09", Releases: official("Bryan Adams: Classic", "2025-05-09"),
	}
	details["9857887e-aa9a-47b0-868a-0f51049cdaed"] = discoveryports.MBRecordingDetail{
		MBID: "9857887e-aa9a-47b0-868a-0f51049cdaed", Title: "It’s Only Love (classic version)", ArtistIDs: []string{bryanAdams},
		FirstReleaseDate: "2025-05-09", Releases: official("Classic", "2025-05-09"),
	}
	details[itsOnlyLoveClassi] = discoveryports.MBRecordingDetail{
		MBID: itsOnlyLoveClassi, Title: "It’s Only Love (classic version)", ArtistIDs: []string{bryanAdams},
		ISRCs: []string{"IETLH2500023"}, Duration: 194, FirstReleaseDate: "2025-04-25", Releases: official("It’s Only Love", "2025-04-25"),
	}
	return &stubRerecordingAuthority{work: work, details: details}
}

func findRerecording(t *testing.T, authority *stubRerecordingAuthority) (acqports.RecordingLabel, bool) {
	t.Helper()
	resolver := NewRecordingResolver(nil, WithRerecordingAuthority(authority))
	label, found, err := resolver.FindRerecording(context.Background(), acqports.RerecordingQuery{OriginalMBID: itsOnlyLove})
	if err != nil {
		t.Fatalf("FindRerecording: %v", err)
	}
	return label, found
}

func TestFindRerecording_BryanAdamsItsOnlyLoveTakesTheClassicVersionWithAnISRC(t *testing.T) {
	logs := captureDefaultLog(t)

	label, found := findRerecording(t, itsOnlyLoveWork())

	want := acqports.RecordingLabel{
		MBID: itsOnlyLoveClassi, Title: "It’s Only Love (classic version)", Album: "It’s Only Love",
		Year: 2025, ISRC: "IETLH2500023", DurationSeconds: 194,
	}
	if !found || label != want {
		t.Fatalf("FindRerecording = %+v, %v; want %+v, true", label, found, want)
	}
	for _, field := range []string{"acquisition.rerecording_found", "rerecording_mbid=" + itsOnlyLoveClassi, "candidates=3"} {
		if !strings.Contains(logs.String(), field) {
			t.Errorf("log lacks %q:\n%s", field, logs.String())
		}
	}
}

func onlyRecording(rec discoveryports.MBWorkRecording, detail discoveryports.MBRecordingDetail) *stubRerecordingAuthority {
	return &stubRerecordingAuthority{
		work:    []discoveryports.MBWorkRecording{rec},
		details: map[string]discoveryports.MBRecordingDetail{itsOnlyLove: itsOnlyLoveOriginal(), rec.MBID: detail},
	}
}

func laterClassic(mbid, title, disambiguation string, artist string) (discoveryports.MBWorkRecording, discoveryports.MBRecordingDetail) {
	rec := discoveryports.MBWorkRecording{MBID: mbid, Title: title, Disambiguation: disambiguation, ArtistIDs: []string{artist}}
	detail := discoveryports.MBRecordingDetail{
		MBID: mbid, Title: title, ArtistIDs: []string{artist}, ISRCs: []string{"IETLH2500099"},
		FirstReleaseDate: "2025-04-25", Releases: official("Classic", "2025-04-25"),
	}
	return rec, detail
}

func TestFindRerecording_NoneWhenNoOtherRecordingIsALabelledLaterStudioRerecordingByTheSameArtist(t *testing.T) {
	liveByAttribute, liveByAttributeDetail := laterClassic("live-attr", "It’s Only Love (classic version)", "", bryanAdams)
	liveByAttribute.Live = true
	liveByTitle, liveByTitleDetail := laterClassic("live-title", "It’s Only Love (classic version) (live at The Royal Albert Hall 2024)", "", bryanAdams)
	liveInDisambiguation, liveInDisambiguationDetail := laterClassic("live-disamb", "It’s Only Love", "classic version, live", bryanAdams)
	unlabelled, unlabelledDetail := laterClassic("unlabelled", "It's Only Love", "", bryanAdams)
	otherLead, otherLeadDetail := laterClassic("other-lead", "It’s Only Love (classic version)", "", otherArtist)
	otherSong, otherSongDetail := laterClassic("other-song", "Heaven (classic version)", "", bryanAdams)
	earlier, earlierDetail := laterClassic("earlier", "It’s Only Love (classic version)", "", bryanAdams)
	earlierDetail.FirstReleaseDate = "1984-01-01"
	undated, undatedDetail := laterClassic("undated", "It’s Only Love (classic version)", "", bryanAdams)
	undatedDetail.FirstReleaseDate = ""
	promoOnly, promoOnlyDetail := laterClassic("promo", "It’s Only Love (classic version)", "", bryanAdams)
	promoOnlyDetail.Releases = []discoveryports.MBRelease{{Title: "Classic", Date: "2025-04-25", Status: "Promotion"}}
	noArtist, noArtistDetail := laterClassic("no-artist", "It’s Only Love (classic version)", "", bryanAdams)
	noArtist.ArtistIDs = nil

	for name, authority := range map[string]*stubRerecordingAuthority{
		"live by attribute":          onlyRecording(liveByAttribute, liveByAttributeDetail),
		"live by title":              onlyRecording(liveByTitle, liveByTitleDetail),
		"live in disambiguation":     onlyRecording(liveInDisambiguation, liveInDisambiguationDetail),
		"no re-recording label":      onlyRecording(unlabelled, unlabelledDetail),
		"another lead artist":        onlyRecording(otherLead, otherLeadDetail),
		"another song of the work":   onlyRecording(otherSong, otherSongDetail),
		"released before original":   onlyRecording(earlier, earlierDetail),
		"no release date":            onlyRecording(undated, undatedDetail),
		"no official release":        onlyRecording(promoOnly, promoOnlyDetail),
		"no artist credit":           onlyRecording(noArtist, noArtistDetail),
		"only the original listed":   {work: itsOnlyLoveWork().work[:1], details: itsOnlyLoveWork().details},
		"unknown original":           {work: itsOnlyLoveWork().work},
		"golden without the classic": {work: itsOnlyLoveWork().work[2:5], details: itsOnlyLoveWork().details},
	} {
		t.Run(name, func(t *testing.T) {
			if label, found := findRerecording(t, authority); found {
				t.Errorf("FindRerecording = %+v, true; want none", label)
			}
		})
	}
}

func TestFindRerecording_NoneWithoutARerecordingAuthority(t *testing.T) {
	resolver := NewRecordingResolver(nil)

	label, found, err := resolver.FindRerecording(context.Background(), acqports.RerecordingQuery{OriginalMBID: itsOnlyLove})

	if err != nil || found || label != (acqports.RecordingLabel{}) {
		t.Fatalf("FindRerecording = %+v, %v, %v; want zero, false, nil", label, found, err)
	}
}

func TestFindRerecording_LogsNoneWithTheCandidateCount(t *testing.T) {
	logs := captureDefaultLog(t)
	authority := itsOnlyLoveWork()
	authority.work = authority.work[:6]
	authority.details["e5cdd7d1-727a-4a5d-9318-11a1cb8e19dc"] = discoveryports.MBRecordingDetail{}
	delete(authority.details, "9857887e-aa9a-47b0-868a-0f51049cdaed")

	if _, found := findRerecording(t, authority); found {
		t.Fatal("FindRerecording found a re-recording; want none")
	}
	for _, field := range []string{"acquisition.rerecording_none", "original_mbid=" + itsOnlyLove, "candidates=2"} {
		if !strings.Contains(logs.String(), field) {
			t.Errorf("log lacks %q:\n%s", field, logs.String())
		}
	}
}

func TestFindRerecording_PrefersAnISRCThenTheEarliestReleaseThenMusicBrainzOrder(t *testing.T) {
	first, firstDetail := laterClassic("first", "It’s Only Love (Re-Recorded)", "", bryanAdams)
	firstDetail.ISRCs, firstDetail.FirstReleaseDate = nil, "2020-01-01"
	second, secondDetail := laterClassic("second", "It’s Only Love", "re-recording", bryanAdams)
	secondDetail.FirstReleaseDate = "2024-06-01"
	third, thirdDetail := laterClassic("third", "It’s Only Love (Rerecorded)", "", bryanAdams)
	thirdDetail.FirstReleaseDate = "2023-03-01"
	fourth, fourthDetail := laterClassic("fourth", "It’s Only Love (Taylor’s Version)", "", bryanAdams)
	fourthDetail.FirstReleaseDate = "2023-03-01"
	authority := &stubRerecordingAuthority{
		work: []discoveryports.MBWorkRecording{first, second, third, fourth},
		details: map[string]discoveryports.MBRecordingDetail{
			itsOnlyLove: itsOnlyLoveOriginal(), "first": firstDetail, "second": secondDetail, "third": thirdDetail, "fourth": fourthDetail,
		},
	}

	label, found := findRerecording(t, authority)

	if !found || label.MBID != "third" {
		t.Fatalf("FindRerecording = %+v, %v; want third (ISRC, earliest, first listed)", label, found)
	}
}

func TestFindRerecording_LabelTakesTheEarliestOfficialRelease(t *testing.T) {
	rec, detail := laterClassic("multi", "It’s Only Love (classic version)", "", bryanAdams)
	detail.Releases = []discoveryports.MBRelease{
		{Title: "Bootleg", Date: "2024-01-01", Status: "Bootleg"},
		{Title: "Undated", Status: "Official"},
		{Title: "Later", Date: "2026-02-02", Status: "Official"},
		{Title: "Earliest", Date: "2025-04-25", Status: "Official"},
	}

	label, found := findRerecording(t, onlyRecording(rec, detail))

	if !found || label.Album != "Earliest" || label.Year != 2025 {
		t.Fatalf("FindRerecording = %+v, %v; want album Earliest, year 2025", label, found)
	}
}

func TestFindRerecording_LooksUpAtMostFiveCandidatesBesideTheOriginal(t *testing.T) {
	authority := &stubRerecordingAuthority{details: map[string]discoveryports.MBRecordingDetail{itsOnlyLove: itsOnlyLoveOriginal()}}
	for i := range 40 {
		rec, detail := laterClassic(fmt.Sprintf("classic-%d", i), "It’s Only Love (classic version)", "", bryanAdams)
		detail.Releases = nil
		authority.work = append(authority.work, rec)
		authority.details[rec.MBID] = detail
	}

	findRerecording(t, authority)

	if authority.detailCalls != 1+maxRerecordingLookups || maxRerecordingLookups != 5 {
		t.Fatalf("RecordingDetail calls = %d with cap %d; want 1 + 5", authority.detailCalls, maxRerecordingLookups)
	}
}

func TestFindRerecording_MusicBrainzErrorsAreReturned(t *testing.T) {
	boom := errors.New("musicbrainz 503")
	originalFails := itsOnlyLoveWork()
	originalFails.detailErr = map[string]error{itsOnlyLove: boom}
	workFails := itsOnlyLoveWork()
	workFails.workErr = boom
	candidateFails := itsOnlyLoveWork()
	candidateFails.detailErr = map[string]error{itsOnlyLoveClassi: boom}

	for name, authority := range map[string]*stubRerecordingAuthority{
		"original detail": originalFails, "work recordings": workFails, "candidate detail": candidateFails,
	} {
		t.Run(name, func(t *testing.T) {
			resolver := NewRecordingResolver(nil, WithRerecordingAuthority(authority))
			_, found, err := resolver.FindRerecording(context.Background(), acqports.RerecordingQuery{OriginalMBID: itsOnlyLove})
			if !errors.Is(err, boom) || found {
				t.Fatalf("FindRerecording = found %v, err %v; want the MusicBrainz error", found, err)
			}
		})
	}
}

func TestFindRerecording_NoneWhenTheLabelledRecordingCameOutTheSameDayAsTheOriginal(t *testing.T) {
	rec, detail := laterClassic("same-day", "It’s Only Love (classic version)", "", bryanAdams)
	detail.FirstReleaseDate = "1984-10-29"
	detail.Releases = official("Reckless", "1984-10-29")

	if label, found := findRerecording(t, onlyRecording(rec, detail)); found {
		t.Fatalf("FindRerecording = %+v, true; want none for a release that does not sort after the original", label)
	}
}

func TestFindRerecording_FollowsTheLeadArtistOfTheCreditNotItsOtherNames(t *testing.T) {
	const tinaTurner = "00000000-0000-0000-0000-0000000000bb"
	duet, duetDetail := laterClassic("duet", "It’s Only Love (classic version)", "", bryanAdams)
	duet.ArtistIDs = []string{bryanAdams, tinaTurner}
	duetDetail.ArtistIDs = duet.ArtistIDs
	featuring, featuringDetail := laterClassic("featuring", "It’s Only Love (classic version)", "", tinaTurner)
	featuring.ArtistIDs = []string{tinaTurner, bryanAdams}
	featuringDetail.ArtistIDs = featuring.ArtistIDs

	t.Run("Bryan Adams & Tina Turner counts", func(t *testing.T) {
		label, found := findRerecording(t, onlyRecording(duet, duetDetail))
		if !found || label.MBID != "duet" {
			t.Fatalf("FindRerecording = %+v, %v; want duet, true", label, found)
		}
	})
	t.Run("Tina Turner feat. Bryan Adams does not", func(t *testing.T) {
		if label, found := findRerecording(t, onlyRecording(featuring, featuringDetail)); found {
			t.Fatalf("FindRerecording = %+v, true; want none for another lead artist", label)
		}
	})
}

func TestFindRerecording_TakesATaylorsVersionWrittenWithAStraightApostropheInCapitals(t *testing.T) {
	rec, detail := laterClassic("tv", "IT'S ONLY LOVE (TAYLOR'S VERSION)", "", bryanAdams)
	detail.Releases = official("Only Love (Taylor's Version)", "2025-04-25")

	label, found := findRerecording(t, onlyRecording(rec, detail))

	want := acqports.RecordingLabel{
		MBID: "tv", Title: "IT'S ONLY LOVE (TAYLOR'S VERSION)", Album: "Only Love (Taylor's Version)",
		Year: 2025, ISRC: "IETLH2500099",
	}
	if !found || label != want {
		t.Fatalf("FindRerecording = %+v, %v; want %+v, true", label, found, want)
	}
}

func TestFindRerecording_NoneForARemasterOfTheOriginal(t *testing.T) {
	for _, title := range []string{"It’s Only Love (Remastered 2014)", "It’s Only Love - 2004 Remaster"} {
		t.Run(title, func(t *testing.T) {
			rec, detail := laterClassic("remaster", title, "", bryanAdams)
			if label, found := findRerecording(t, onlyRecording(rec, detail)); found {
				t.Fatalf("FindRerecording = %+v, true; want none for a recording without a re-recording label", label)
			}
		})
	}
}

func TestFindRerecording_SkipsACandidateMusicBrainzNoLongerKnowsAndTakesTheNext(t *testing.T) {
	gone, _ := laterClassic("gone", "It’s Only Love (classic version)", "", bryanAdams)
	kept, keptDetail := laterClassic("kept", "It’s Only Love (classic version)", "", bryanAdams)
	authority := &stubRerecordingAuthority{
		work:    []discoveryports.MBWorkRecording{gone, kept},
		details: map[string]discoveryports.MBRecordingDetail{itsOnlyLove: itsOnlyLoveOriginal(), "kept": keptDetail},
	}

	label, found := findRerecording(t, authority)

	if !found || label.MBID != "kept" {
		t.Fatalf("FindRerecording = %+v, %v; want kept, true", label, found)
	}
}

func TestFindRerecording_AYearOnlyReleaseDateAfterTheOriginalCounts(t *testing.T) {
	rec, detail := laterClassic("year-only", "It’s Only Love (classic version)", "", bryanAdams)
	detail.FirstReleaseDate = "2025"
	detail.Releases = official("Classic", "2025")

	label, found := findRerecording(t, onlyRecording(rec, detail))

	if !found || label.MBID != "year-only" || label.Year != 2025 || label.Album != "Classic" {
		t.Fatalf("FindRerecording = %+v, %v; want year-only, Classic, 2025, true", label, found)
	}
}

func TestFindRerecording_NoneAndNoPanicWhenTheOriginalHasNoArtistCredit(t *testing.T) {
	rec, detail := laterClassic("classic", "It’s Only Love (classic version)", "", bryanAdams)
	authority := onlyRecording(rec, detail)
	original := itsOnlyLoveOriginal()
	original.ArtistIDs = nil
	authority.details[itsOnlyLove] = original

	if label, found := findRerecording(t, authority); found {
		t.Fatalf("FindRerecording = %+v, true; want none when the original names no lead artist", label)
	}
}

func TestFindRerecording_TheSameWorkGivesTheSameAnswerEveryTime(t *testing.T) {
	for i := range 20 {
		label, found := findRerecording(t, itsOnlyLoveWork())
		if !found || label.MBID != itsOnlyLoveClassi {
			t.Fatalf("run %d: FindRerecording = %+v, %v; want %s", i, label, found, itsOnlyLoveClassi)
		}
	}
}
