package main

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeLister struct {
	songs map[string]int
}

func (f fakeLister) Artists(context.Context) ([]string, error) {
	return []string{"b", "a", "c", "broken"}, nil
}

func (f fakeLister) ArtistSongs(_ context.Context, name string) ([]domain.UnreleasedSong, error) {
	if name == "broken" {
		return nil, errors.New("tracker sheet: http status 429")
	}
	return make([]domain.UnreleasedSong, f.songs[name]), nil
}

func TestRun_SummarisesTheHubAndListsArtistsByCountThenName(t *testing.T) {
	var out strings.Builder
	if err := run(context.Background(), fakeLister{songs: map[string]int{"a": 2, "b": 2, "c": 0}}, &out, 0, 0); err != nil {
		t.Fatal(err)
	}
	want := "artists 4, with songs 2, empty 1, errors 1, songs 4\na\t2\nb\t2\nbroken\terror: tracker sheet: http status 429\nc\t0\n"
	if out.String() != want {
		t.Errorf("report =\n%s\nwant\n%s", out.String(), want)
	}
}
