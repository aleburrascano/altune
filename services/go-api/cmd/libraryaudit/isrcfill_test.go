package main

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

type fakeRecordings struct {
	fakeCatalog
	searched []recording
	byISRC   map[string][]recording
	err      error
}

func (f fakeRecordings) SearchRecordings(context.Context, string, string) ([]recording, error) {
	return f.searched, f.err
}

func (f fakeRecordings) RecordingsByISRC(_ context.Context, isrc string) ([]recording, error) {
	return f.byISRC[isrc], f.err
}

func deezerFinds(isrc string) fakeCatalog {
	return fakeCatalog{
		search: []catalogTrack{{ID: 2, Title: "Valentina", Artist: "Daniel Caesar", DurationSeconds: 236}},
		byID:   map[int64]catalogTrack{2: {ID: 2, ISRC: isrc}},
	}
}

func valentina(isrcs ...string) recording {
	return recording{ID: "mbid-1", Title: "Valentina", Artists: []string{"Daniel Caesar"}, DurationSeconds: 237, ISRCs: isrcs}
}

func TestAuditTrack_NoISRCGetsAFixWhenDeezerAndMusicBrainzAgree(t *testing.T) {
	lib := libTrack()
	lib.UserID, lib.Version = "u1", 7
	cat := fakeRecordings{fakeCatalog: deezerFinds("USFOUND00001"), searched: []recording{valentina("usfound00001")}}
	got := auditTrack(context.Background(), cat, fakeCovers{}, lib)
	found := findingOf(got.Findings, categoryISRCFillable)
	want := fix{UserID: "u1", Version: 7, Field: "isrc", NewValue: strPtr("USFOUND00001"), Sources: []string{"deezer", "musicbrainz"}}
	if found.Fix == nil || !reflect.DeepEqual(*found.Fix, want) || findingOf(got.Findings, categoryNoISRC).Detail != "" {
		t.Fatalf("got %+v", got.Findings)
	}
}

func TestAuditTrack_NoISRCStaysReportOnlyWithTheReasonWhenMusicBrainzDoesNotAgree(t *testing.T) {
	otherSong := recording{ID: "m", Title: "Ransom", Artists: []string{"Lil Tecca"}, DurationSeconds: 236, ISRCs: []string{"USFOUND00001"}}
	wrongLength := recording{ID: "m", Title: "Valentina", Artists: []string{"Daniel Caesar"}, DurationSeconds: 120, ISRCs: []string{"USFOUND00001"}}
	cases := []struct {
		name   string
		cat    fakeRecordings
		reason string
	}{
		{"musicbrainz lists another isrc", fakeRecordings{searched: []recording{valentina("USOTHER00001")}}, "listing USFOUND00001"},
		{"musicbrainz has no recording", fakeRecordings{}, "listing USFOUND00001"},
		{"matching isrc on a different song", fakeRecordings{searched: []recording{otherSong}}, "listing USFOUND00001"},
		{"matching isrc on a recording of the wrong length", fakeRecordings{searched: []recording{wrongLength}}, "listing USFOUND00001"},
		{"musicbrainz lookup fails", fakeRecordings{err: errors.New("boom")}, "musicbrainz lookup failed: boom"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.cat.fakeCatalog = deezerFinds("USFOUND00001")
			got := auditTrack(context.Background(), tc.cat, fakeCovers{}, libTrack())
			noISRC, fillable := findingOf(got.Findings, categoryNoISRC), findingOf(got.Findings, categoryISRCFillable)
			if fillable.Fix != nil || fillable.Detail != "USFOUND00001" || !strings.Contains(noISRC.Detail, tc.reason) || got.Blocked != nil {
				t.Fatalf("got %+v", got.Findings)
			}
		})
	}
}

func TestAuditTrack_NoISRCWithoutAUniqueDeezerCandidateNeverReachesAFix(t *testing.T) {
	two := fakeCatalog{search: []catalogTrack{
		{ID: 1, Title: "Valentina", Artist: "Daniel Caesar", DurationSeconds: 236},
		{ID: 2, Title: "Valentina - Remastered", Artist: "Daniel Caesar", DurationSeconds: 238},
	}}
	for name, cat := range map[string]fakeCatalog{"ambiguous": two, "none": {}} {
		got := auditTrack(context.Background(), fakeRecordings{fakeCatalog: cat, searched: []recording{valentina("ISRC1", "ISRC2")}}, fakeCovers{}, libTrack())
		for _, f := range got.Findings {
			if f.Fix != nil {
				t.Fatalf("%s: got a fix %+v", name, f)
			}
		}
	}
}

func TestAuditTrack_NoISRCWithoutAMusicBrainzSourceIsReportOnly(t *testing.T) {
	got := auditTrack(context.Background(), deezerFinds("USFOUND00001"), fakeCovers{}, libTrack())
	if findingOf(got.Findings, categoryISRCFillable).Fix != nil || !strings.Contains(findingOf(got.Findings, categoryNoISRC).Detail, "not consulted") {
		t.Fatalf("got %+v", got.Findings)
	}
}

func TestAuditTrack_MusicBrainzBlockIsABlock(t *testing.T) {
	cat := fakeRecordings{fakeCatalog: deezerFinds("USFOUND00001"), err: blocked("musicbrainz 503")}
	if got := auditTrack(context.Background(), cat, fakeCovers{}, libTrack()); !errors.Is(got.Blocked, errCatalogBlocked) {
		t.Fatalf("got %+v", got)
	}
	lib := libTrack()
	lib.ISRC = "NOPE"
	if got := auditTrack(context.Background(), cat, fakeCovers{}, lib); !errors.Is(got.Blocked, errCatalogBlocked) {
		t.Fatalf("unknown isrc: got %+v", got)
	}
}

func TestAuditTrack_UnknownISRCIsKeptWhenMusicBrainzKnowsItForTheSameSong(t *testing.T) {
	lib := libTrack()
	lib.ISRC = "NOPE"
	cat := fakeRecordings{byISRC: map[string][]recording{"NOPE": {valentina("NOPE")}}}
	got := auditTrack(context.Background(), cat, fakeCovers{}, lib)
	if len(got.Findings) != 1 || got.Findings[0].Category != categoryISRCConfirmed || got.Findings[0].Fix != nil {
		t.Fatalf("got %+v", got.Findings)
	}
}

func TestAuditTrack_UnknownISRCIsReportedWhenMusicBrainzDoesNotConfirmIt(t *testing.T) {
	otherSong := recording{ID: "m", Title: "Ransom", Artists: []string{"Lil Tecca"}, DurationSeconds: 236}
	cases := map[string]fakeRecordings{
		"musicbrainz does not know it": {},
		"it belongs to another song":   {byISRC: map[string][]recording{"NOPE": {otherSong}}},
		"lookup fails":                 {err: errors.New("boom")},
	}
	for name, cat := range cases {
		t.Run(name, func(t *testing.T) {
			lib := libTrack()
			lib.ISRC = "NOPE"
			got := auditTrack(context.Background(), cat, fakeCovers{}, lib)
			if len(got.Findings) != 1 || got.Findings[0].Category != categoryUnknownISRC || got.Findings[0].Fix != nil {
				t.Fatalf("got %+v", got.Findings)
			}
		})
	}
}
