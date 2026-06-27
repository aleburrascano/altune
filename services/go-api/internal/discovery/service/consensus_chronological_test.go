package service

import (
	"testing"

	"altune/go-api/internal/discovery/domain"
)

func chronoAlbum(title string, year any) ConsensusAlbum {
	extras := map[string]any{}
	if year != nil {
		extras["year"] = year
	}
	return ConsensusAlbum{
		Album:  res(domain.ResultKindAlbum, title, "Artist", domain.ProviderDeezer, extras),
		Status: ConsensusConfirmed,
	}
}

func TestSortChronological_NewestFirstUnknownLast(t *testing.T) {
	in := []ConsensusAlbum{
		chronoAlbum("Old", 2018),
		chronoAlbum("Unknown", nil),
		chronoAlbum("Newest", 2024),
		chronoAlbum("Mid", 2021),
		chronoAlbum("StringYear", "2020"),
	}

	sortChronological(in)

	got := make([]string, len(in))
	for i, a := range in {
		got[i] = a.Album.Title
	}
	want := []string{"Newest", "Mid", "StringYear", "Old", "Unknown"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

func TestYearOf(t *testing.T) {
	cases := []struct {
		name string
		v    any
		want int
	}{
		{"int", 2020, 2020},
		{"int64", int64(2019), 2019},
		{"float64", float64(2021), 2021},
		{"string", "2018", 2018},
		{"string padded", " 2017 ", 2017},
		{"missing", nil, 0},
		{"garbage", "n/a", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			extras := map[string]any{}
			if c.v != nil {
				extras["year"] = c.v
			}
			r := res(domain.ResultKindAlbum, "X", "Y", domain.ProviderDeezer, extras)
			if got := yearOf(r); got != c.want {
				t.Errorf("yearOf(%v) = %d, want %d", c.v, got, c.want)
			}
		})
	}
}
