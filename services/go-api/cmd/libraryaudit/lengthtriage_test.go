package main

import (
	"context"
	"errors"
	"testing"
)

type isrcRecordings map[string][]recording

func (f isrcRecordings) RecordingsByISRC(_ context.Context, isrc string) ([]recording, error) {
	found, ok := f[isrc]
	if !ok {
		return nil, errCatalogUnknown
	}
	stamped := make([]recording, 0, len(found))
	for _, rec := range found {
		if len(rec.ISRCs) == 0 {
			rec.ISRCs = []string{isrc}
		}
		stamped = append(stamped, rec)
	}
	return stamped, nil
}

func radioEditTrack() libraryTrack {
	return libraryTrack{ID: "t1", UserID: "u1", Version: 3, Title: "1999", Artist: "Prince", Album: "The Hits 1", ISRC: "USALBUM", DurationSeconds: 226}
}

func albumVersion() catalogTrack {
	return catalogTrack{ID: 1, Title: "1999", Artist: "Prince", Album: "1999", DurationSeconds: 379, ISRC: "USALBUM"}
}

func radioEditVersion(id int64, isrc string) catalogTrack {
	return catalogTrack{ID: id, Title: "1999", Artist: "Prince", Album: "The Hits 1", DurationSeconds: 225, ISRC: isrc}
}

func triaged(t *testing.T, cat fakeCatalog, recordings isrcRecordings, lib libraryTrack) []finding {
	t.Helper()
	cat.byISRC = map[string]catalogTrack{lib.ISRC: albumVersion()}
	lengthFinding := about(lib, categoryLengthShorter, "")
	got, err := triageLengthFindings(context.Background(), cat, recordings, []libraryTrack{lib}, []finding{lengthFinding})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func princeMusicBrainz(isrc string) isrcRecordings {
	return isrcRecordings{isrc: {{Title: "1999 (Single Edit)", Artists: []string{"Prince"}, DurationSeconds: 226}}}
}

func TestTriageLengthFindings_OneFittingVersionConfirmedByMusicBrainzFixesTheISRC(t *testing.T) {
	cat := fakeCatalog{
		search: []catalogTrack{albumVersion(), {ID: 2, Title: "1999", Artist: "Prince", Album: "The Hits 1", DurationSeconds: 225}},
		byID:   map[int64]catalogTrack{2: radioEditVersion(2, "USRADIO")},
	}
	got := triaged(t, cat, princeMusicBrainz("USRADIO"), radioEditTrack())
	plan := got[len(got)-1].LengthPlan
	if len(got) != 2 || got[1].Category != categoryLengthVersionFix || plan == nil ||
		plan.Action != actionFixISRC || plan.NewISRC != "USRADIO" || plan.OldISRC != "USALBUM" ||
		plan.CatalogSeconds != 225 || plan.FileSeconds != 226 || plan.Version != 3 || plan.UserID != "u1" {
		t.Fatalf("got %+v", got)
	}
}

func TestTriageLengthFindings_NoFittingVersionMeansReacquire(t *testing.T) {
	cat := fakeCatalog{search: []catalogTrack{albumVersion(), {ID: 3, Title: "1999", Artist: "Prince", DurationSeconds: 300}}}
	got := triaged(t, cat, isrcRecordings{}, radioEditTrack())
	plan := got[len(got)-1].LengthPlan
	if got[1].Category != categoryLengthReacquire || plan.Action != actionReacquire || plan.NewISRC != "" || plan.CatalogSeconds != 379 {
		t.Fatalf("got %+v", got)
	}
}

func TestTriageLengthFindings_TwoFittingVersionsMeansReacquire(t *testing.T) {
	cat := fakeCatalog{
		search: []catalogTrack{{ID: 2, Title: "1999", Artist: "Prince", Album: "The Hits 1", DurationSeconds: 225}, {ID: 4, Title: "1999", Artist: "Prince", Album: "The Hits 1", DurationSeconds: 228}},
		byID:   map[int64]catalogTrack{2: radioEditVersion(2, "USRADIO"), 4: radioEditVersion(4, "USOTHER")},
	}
	recordings := isrcRecordings{
		"USRADIO": {{Title: "1999", Artists: []string{"Prince"}, DurationSeconds: 226}},
		"USOTHER": {{Title: "1999", Artists: []string{"Prince"}, DurationSeconds: 226}},
	}
	got := triaged(t, cat, recordings, radioEditTrack())
	if got[1].Category != categoryLengthReacquire {
		t.Fatalf("got %+v", got)
	}
}

func TestTriageLengthFindings_SameISRCUnderTwoDeezerIDsIsOneVersion(t *testing.T) {
	cat := fakeCatalog{
		search: []catalogTrack{{ID: 2, Title: "1999", Artist: "Prince", Album: "The Hits 1", DurationSeconds: 225}, {ID: 5, Title: "1999", Artist: "Prince", Album: "The Hits 1", DurationSeconds: 225}},
		byID:   map[int64]catalogTrack{2: radioEditVersion(2, "USRADIO"), 5: radioEditVersion(5, "usradio")},
	}
	got := triaged(t, cat, princeMusicBrainz("USRADIO"), radioEditTrack())
	if got[1].Category != categoryLengthVersionFix {
		t.Fatalf("got %+v", got)
	}
}

func TestTriageLengthFindings_MusicBrainzDisagreeingMeansReacquire(t *testing.T) {
	cases := map[string]isrcRecordings{
		"isrc unknown to musicbrainz": {},
		"length differs":              {"USRADIO": {{Title: "1999", Artists: []string{"Prince"}, DurationSeconds: 379}}},
		"another song":                {"USRADIO": {{Title: "Purple Rain", Artists: []string{"Prince"}, DurationSeconds: 226}}},
		"another artist":              {"USRADIO": {{Title: "1999", Artists: []string{"Charlie Wilson"}, DurationSeconds: 226}}},
		"isrc not on the recording":   {"USRADIO": {{Title: "1999", Artists: []string{"Prince"}, DurationSeconds: 226, ISRCs: []string{"USELSE"}}}},
		"no recorded length":          {"USRADIO": {{Title: "1999", Artists: []string{"Prince"}}}},
	}
	for name, recordings := range cases {
		t.Run(name, func(t *testing.T) {
			cat := fakeCatalog{
				search: []catalogTrack{{ID: 2, Title: "1999", Artist: "Prince", Album: "The Hits 1", DurationSeconds: 225}},
				byID:   map[int64]catalogTrack{2: radioEditVersion(2, "USRADIO")},
			}
			got := triaged(t, cat, recordings, radioEditTrack())
			if got[1].Category != categoryLengthReacquire {
				t.Fatalf("got %+v", got)
			}
		})
	}
}

type blockedRecordings struct{}

func (blockedRecordings) RecordingsByISRC(context.Context, string) ([]recording, error) {
	return nil, blocked("test")
}

func TestTriageLengthFindings_BlockedMusicBrainzAbortsInsteadOfGuessing(t *testing.T) {
	lib := radioEditTrack()
	cat := fakeCatalog{
		byISRC: map[string]catalogTrack{lib.ISRC: albumVersion()},
		search: []catalogTrack{{ID: 2, Title: "1999", Artist: "Prince", Album: "The Hits 1", DurationSeconds: 225}},
		byID:   map[int64]catalogTrack{2: radioEditVersion(2, "USRADIO")},
	}
	got, err := triageLengthFindings(context.Background(), cat, blockedRecordings{}, []libraryTrack{lib}, []finding{about(lib, categoryLengthShorter, "")})
	if !errors.Is(err, errCatalogBlocked) || len(got) != 1 {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestTriageLengthFindings_LookupFailureIsReportedNotReacquired(t *testing.T) {
	lib := radioEditTrack()
	cat := fakeCatalog{failISRC: map[string]error{lib.ISRC: errors.New("boom")}}
	got, err := triageLengthFindings(context.Background(), cat, isrcRecordings{}, []libraryTrack{lib}, []finding{about(lib, categoryLengthShorter, "")})
	if err != nil || len(got) != 2 || got[1].Category != categoryLookupFailed || got[1].LengthPlan != nil {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestTriageLengthFindings_LeavesOtherFindingsUntouched(t *testing.T) {
	lib := radioEditTrack()
	other := about(lib, categoryCoverURLDiffers, "x")
	got, err := triageLengthFindings(context.Background(), fakeCatalog{}, isrcRecordings{}, []libraryTrack{lib}, []finding{other})
	if err != nil || len(got) != 1 || got[0] != other {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestTriageLengthFindings_AVersionThatOnlyFitsOurFileIsNotTheUsersPick(t *testing.T) {
	cat := fakeCatalog{
		search: []catalogTrack{albumVersion(), {ID: 2, Title: "1999", Artist: "Prince", Album: "Ultimate", DurationSeconds: 225}},
		byID:   map[int64]catalogTrack{2: radioEditVersion(2, "USRADIO")},
	}
	got := triaged(t, cat, princeMusicBrainz("USRADIO"), radioEditTrack())
	if got[1].Category != categoryLengthReacquire {
		t.Fatalf("got %+v", got)
	}
}
