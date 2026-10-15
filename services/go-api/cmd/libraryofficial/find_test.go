package main

import (
	"altune/go-api/internal/acquisition/adapters/ytmsearch"
	"context"
	"errors"
	"reflect"
	"testing"
)

type scriptedSearch struct {
	answers map[string][]ytmsearch.Song
	err     error
	asked   []string
}

func (s *scriptedSearch) SearchSongs(_ context.Context, query string) ([]ytmsearch.Song, error) {
	s.asked = append(s.asked, query)
	return s.answers[query], s.err
}

func otherArtTrack(videoID string) ytmsearch.Song {
	song := artTrack()
	song.VideoID = videoID
	return song
}

const textQuery = "Cutting Crew (I Just) Died in Your Arms"

func TestFind_IsrcFirst(t *testing.T) {
	track := theTrack()
	search := &scriptedSearch{answers: map[string][]ytmsearch.Song{track.ISRC: {artTrack()}}}
	got := find(t.Context(), search, track)
	if got.Kind != outcomeFound || got.VideoID != "AbCdEfGhIj1" {
		t.Fatalf("got %+v", got)
	}
	if !reflect.DeepEqual(search.asked, []string{track.ISRC}) {
		t.Fatalf("asked %v, want the ISRC only", search.asked)
	}
}

func TestFind_FallsBackToArtistAndTitle(t *testing.T) {
	track := theTrack()
	search := &scriptedSearch{answers: map[string][]ytmsearch.Song{textQuery: {artTrack()}}}
	got := find(t.Context(), search, track)
	if got.Kind != outcomeFound {
		t.Fatalf("got %+v", got)
	}
	if !reflect.DeepEqual(search.asked, []string{track.ISRC, textQuery}) {
		t.Fatalf("asked %v", search.asked)
	}
}

func TestFind_WithoutIsrcAsksText(t *testing.T) {
	track := theTrack()
	track.ISRC = ""
	search := &scriptedSearch{}
	find(t.Context(), search, track)
	if !reflect.DeepEqual(search.asked, []string{textQuery}) {
		t.Fatalf("asked %v", search.asked)
	}
}

func TestFind_TwoDifferentArtTracksAreAmbiguous(t *testing.T) {
	track := theTrack()
	search := &scriptedSearch{answers: map[string][]ytmsearch.Song{track.ISRC: {artTrack(), otherArtTrack("ZzCdEfGhIj2"), artTrack()}}}
	got := find(t.Context(), search, track)
	if got.Kind != outcomeAmbiguous || got.VideoID != "" || len(got.VideoIDs) != 2 {
		t.Fatalf("got %+v", got)
	}
}

func TestFind_OnlyRejectedCandidatesIsNoneWithReasons(t *testing.T) {
	track := theTrack()
	upload := artTrack()
	upload.Type = ytmsearch.VideoTypeUGC
	tooLong := artTrack()
	tooLong.DurationSeconds = 400
	search := &scriptedSearch{answers: map[string][]ytmsearch.Song{track.ISRC: {upload}, textQuery: {tooLong}}}
	got := find(t.Context(), search, track)
	if got.Kind != outcomeNone || !reflect.DeepEqual(got.Reasons, []rejection{rejectNotArtTrack, rejectLength}) {
		t.Fatalf("got %+v", got)
	}
}

func TestFind_OnlyTheTopResultsAreConsidered(t *testing.T) {
	track := theTrack()
	filler := artTrack()
	filler.Type = ytmsearch.VideoTypeUGC
	songs := []ytmsearch.Song{filler, filler, filler, filler, filler, artTrack()}
	search := &scriptedSearch{answers: map[string][]ytmsearch.Song{track.ISRC: songs}}
	if got := find(t.Context(), search, track); got.Kind != outcomeNone {
		t.Fatalf("got %+v", got)
	}
}

func TestFind_ASearchFailureIsReported(t *testing.T) {
	boom := errors.New("429")
	got := find(t.Context(), &scriptedSearch{err: boom}, theTrack())
	if got.Kind != outcomeErrored || !errors.Is(got.Err, boom) {
		t.Fatalf("got %+v", got)
	}
}
