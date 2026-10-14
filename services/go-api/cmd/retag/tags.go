package main

import (
	"altune/go-api/internal/acquisition/ports"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

type tagDiff struct {
	Field string
	Want  string
	Got   string
}

func yearText(year int) string {
	if year <= 0 {
		return ""
	}
	return strconv.Itoa(year)
}

func tagTexts(tags ports.TrackTags) [][2]string {
	return [][2]string{
		{"title", tags.Title},
		{"artist", tags.Artist},
		{"album", tags.Album},
		{"album_artist", tags.AlbumArtist},
		{"year", yearText(tags.Year)},
		{"track_number", yearText(tags.TrackNumber)},
		{"genre", tags.Genre},
	}
}

func diffTags(want, got ports.TrackTags) []tagDiff {
	wantTexts, gotTexts := tagTexts(want), tagTexts(got)
	var diffs []tagDiff
	for i := range wantTexts {
		if wantTexts[i][1] != gotTexts[i][1] {
			diffs = append(diffs, tagDiff{Field: wantTexts[i][0], Want: wantTexts[i][1], Got: gotTexts[i][1]})
		}
	}
	return diffs
}

type probeOutput struct {
	Format struct {
		Tags map[string]string `json:"tags"`
	} `json:"format"`
}

func parseProbeTags(raw []byte) (ports.TrackTags, error) {
	var out probeOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		return ports.TrackTags{}, fmt.Errorf("parse ffprobe output: %w", err)
	}
	tags := make(map[string]string, len(out.Format.Tags))
	for key, value := range out.Format.Tags {
		tags[strings.ToLower(key)] = value
	}
	return ports.TrackTags{
		Title:       tags["title"],
		Artist:      tags["artist"],
		Album:       tags["album"],
		AlbumArtist: tags["album_artist"],
		Genre:       tags["genre"],
		Year:        leadingNumber(tags["date"], "-"),
		TrackNumber: leadingNumber(tags["track"], "/"),
	}, nil
}

func leadingNumber(text, separator string) int {
	head, _, _ := strings.Cut(strings.TrimSpace(text), separator)
	number, err := strconv.Atoi(head)
	if err != nil || number < 0 {
		return 0
	}
	return number
}
