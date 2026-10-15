package main

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"
)

func track(title, artist, album, artwork, source string) unmatchedTrack {
	return unmatchedTrack{ID: "t1", UserID: "u1", Version: 3, Title: title, Artist: artist, Album: album, ArtworkURL: artwork, SourceURL: source}
}

func newValueOf(t *testing.T, found []finding, field string) string {
	t.Helper()
	for _, f := range found {
		if f.Fix.Field == field {
			if f.Fix.NewValue == nil {
				t.Fatalf("%s fix has no new value", field)
			}
			return *f.Fix.NewValue
		}
	}
	t.Fatalf("no %s fix in %+v", field, found)
	return ""
}

func TestHygieneFindings_CollapsesWhitespaceInTitleArtistAndAlbum(t *testing.T) {
	found := hygieneFindings(track("Boost  ", " Lil   Tecca", "UN  RELEASED", "https://a/b.jpg", ""))
	if got := newValueOf(t, found, "title"); got != "Boost" {
		t.Errorf("title = %q", got)
	}
	if got := newValueOf(t, found, "artist"); got != "Lil Tecca" {
		t.Errorf("artist = %q", got)
	}
	if got := newValueOf(t, found, "album"); got != "UN RELEASED" {
		t.Errorf("album = %q", got)
	}
	for _, f := range found {
		if f.Fix.Mode != modeCorrect || f.Fix.OldValue == nil || f.Fix.UserID != "u1" || f.Fix.Version != 3 {
			t.Errorf("fix %+v is not a correct-mode fix tied to the seen version", f.Fix)
		}
	}
}

func TestHygieneFindings_StripsATrailingSourceTagFromTheTitle(t *testing.T) {
	for title, want := range map[string]string{
		"Song (Official Video)":       "Song",
		"Song  (Lyric Video) ":        "Song",
		"Song [HD]":                   "Song",
		"Song [Audio]":                "Song",
		"Song (Official Music Video)": "Song",
	} {
		found := hygieneFindings(track(title, "A", "B", "https://a/b.jpg", ""))
		if got := newValueOf(t, found, "title"); got != want {
			t.Errorf("%q -> %q, want %q", title, got, want)
		}
	}
}

func TestHygieneFindings_LeavesRealTitleParenthesesAlone(t *testing.T) {
	for _, title := range []string{
		"Song (Live at City Hall)", "Song (Remix)", "Song (feat. Someone)", "Song (Deluxe Edition)",
		"Song (Hello)", "Song (Remastered 2012)", "[Official Audio]", "Plain Title",
		"Song (Unreleased Audio)", "Song (Bootleg Audio)", "Song (Mashup Audio)", "Song (Alt Audio)",
		"Song (Kanye Audio)", "Song (Unreleased HD)", "Song [Official Audio]",
	} {
		if found := hygieneFindings(track(title, "A", "B", "https://a/b.jpg", "")); len(found) != 0 {
			t.Errorf("%q changed: %+v", title, found)
		}
	}
}

func TestHygieneFindings_NeverInventsAMissingAlbum(t *testing.T) {
	if found := hygieneFindings(track("Song", "A", "", "https://a/b.jpg", "")); len(found) != 0 {
		t.Errorf("empty album produced %+v", found)
	}
}

func TestHygieneFindings_FillsAMissingCoverFromTheYouTubeSourceThumbnail(t *testing.T) {
	found := hygieneFindings(track("Song", "A", "B", "", "https://music.youtube.com/watch?v=7TDeBi34OtE"))
	if got := newValueOf(t, found, "artwork_url"); got != "https://i.ytimg.com/vi/7TDeBi34OtE/hqdefault.jpg" {
		t.Errorf("cover = %q", got)
	}
	if len(found) != 1 || found[0].Fix.Mode != modeFill || found[0].Fix.OldValue != nil {
		t.Errorf("cover fix %+v is not a fill of an empty cover", found)
	}
}

func TestHygieneFindings_KeepsAnExistingCoverAndIgnoresSourcesWithoutAThumbnail(t *testing.T) {
	for _, tc := range []unmatchedTrack{
		track("Song", "A", "B", "https://a/b.jpg", "https://www.youtube.com/watch?v=7TDeBi34OtE"),
		track("Song", "A", "B", "  ", "https://soundcloud.com/x/y"),
		track("Song", "A", "B", "", "https://www.youtube.com/watch?v=short"),
		track("Song", "A", "B", "", "https://evil.example/watch?v=7TDeBi34OtE"),
		track("Song", "A", "B", "", ""),
	} {
		if found := hygieneFindings(tc); len(found) != 0 {
			t.Errorf("%+v produced %+v", tc, found)
		}
	}
}

func TestBuildReport_CountsFixesByCategoryInTheShapeReconcileReads(t *testing.T) {
	tracks := []unmatchedTrack{
		track("Boost ", "A", "B", "https://a/b.jpg", ""),
		track("Song [Official Video]", "A", "B", "", "https://www.youtube.com/watch?v=7TDeBi34OtE"),
		track("Clean", "A", "B", "https://a/b.jpg", ""),
	}
	rep := buildReport(tracks, time.Unix(0, 0))
	if rep.Tracks != 3 || rep.Summary[categoryWhitespace] != 1 || rep.Summary[categorySourceTag] != 1 || rep.Summary[categoryCover] != 1 {
		t.Fatalf("report = %+v", rep)
	}
	var out bytes.Buffer
	if err := writeReport(&out, rep); err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Findings []struct {
			TrackID string `json:"track_id"`
			Fix     struct {
				UserID   string  `json:"user_id"`
				Version  int     `json:"version"`
				Field    string  `json:"field"`
				Mode     string  `json:"mode"`
				OldValue *string `json:"old_value"`
				NewValue *string `json:"new_value"`
			} `json:"fix"`
		} `json:"findings"`
	}
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Findings) != 3 || decoded.Findings[0].TrackID != "t1" || decoded.Findings[0].Fix.Field != "title" || *decoded.Findings[0].Fix.OldValue != "Boost " {
		t.Errorf("decoded = %+v", decoded)
	}
}

func TestBuildReport_NoTracksWritesAnEmptyFindingsList(t *testing.T) {
	var out bytes.Buffer
	if err := writeReport(&out, buildReport(nil, time.Unix(0, 0))); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out.Bytes(), []byte(`"findings": []`)) {
		t.Errorf("output = %s", out.String())
	}
}
