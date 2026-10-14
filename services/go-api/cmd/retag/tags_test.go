package main

import (
	"altune/go-api/internal/acquisition/ports"
	"reflect"
	"testing"
)

var fullTags = ports.TrackTags{
	Title: "Remember Them Days", Artist: "Slayr", Album: "Remember Them Days",
	AlbumArtist: "Slayr", Year: 2024, TrackNumber: 3, Genre: "Hip-Hop",
}

func TestDiffTags(t *testing.T) {
	cases := []struct {
		name string
		got  ports.TrackTags
		want []tagDiff
	}{
		{"identical", fullTags, nil},
		{
			"title differs", with(fullTags, func(x *ports.TrackTags) { x.Title = "Old" }),
			[]tagDiff{{Field: "title", Want: "Remember Them Days", Got: "Old"}},
		},
		{
			"missing year", with(fullTags, func(x *ports.TrackTags) { x.Year = 0 }),
			[]tagDiff{{Field: "year", Want: "2024", Got: ""}},
		},
		{
			"album artist and genre", with(fullTags, func(x *ports.TrackTags) { x.AlbumArtist = "LUCKI"; x.Genre = "" }),
			[]tagDiff{{Field: "album_artist", Want: "Slayr", Got: "LUCKI"}, {Field: "genre", Want: "Hip-Hop", Got: ""}},
		},
		{
			"track number", with(fullTags, func(x *ports.TrackTags) { x.TrackNumber = 9 }),
			[]tagDiff{{Field: "track_number", Want: "3", Got: "9"}},
		},
		{
			"case matters", with(fullTags, func(x *ports.TrackTags) { x.Artist = "SLAYR" }),
			[]tagDiff{{Field: "artist", Want: "Slayr", Got: "SLAYR"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := diffTags(fullTags, tc.got); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("diffTags = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestDiffTags_EmptyDatabaseValueEqualsAbsentTag(t *testing.T) {
	want := ports.TrackTags{Title: "Gorah", Artist: "Nitefreak"}
	if diffs := diffTags(want, want); len(diffs) != 0 {
		t.Fatalf("diffTags = %+v, want none", diffs)
	}
}

func with(tags ports.TrackTags, edit func(*ports.TrackTags)) ports.TrackTags {
	edit(&tags)
	return tags
}

func TestParseProbeTags(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want ports.TrackTags
	}{
		{
			"m4a keys", `{"format":{"tags":{"title":"T","artist":"A","album":"B","album_artist":"AA","date":"2024","track":"3","genre":"G"}}}`,
			ports.TrackTags{Title: "T", Artist: "A", Album: "B", AlbumArtist: "AA", Year: 2024, TrackNumber: 3, Genre: "G"},
		},
		{
			"upper-case keys with full date and track total", `{"format":{"tags":{"TITLE":"T","DATE":"2023-05-01","TRACK":"4/12"}}}`,
			ports.TrackTags{Title: "T", Year: 2023, TrackNumber: 4},
		},
		{"no tags", `{"format":{}}`, ports.TrackTags{}},
		{"unparseable numbers", `{"format":{"tags":{"date":"soon","track":"x"}}}`, ports.TrackTags{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseProbeTags([]byte(tc.raw))
			if err != nil || got != tc.want {
				t.Fatalf("parseProbeTags = %+v, %v; want %+v", got, err, tc.want)
			}
		})
	}
}

func TestParseProbeTags_RejectsMalformedJSON(t *testing.T) {
	if _, err := parseProbeTags([]byte("not json")); err == nil {
		t.Fatal("expected an error")
	}
}
