package main

import (
	"altune/go-api/internal/acquisition/adapters/ytmsearch"
	"altune/go-api/internal/acquisition/service/grade"
	"context"
	"errors"
	"testing"
)

type fakeLookup struct {
	watch       ytmsearch.Watch
	watchErr    error
	author      string
	authorErr   error
	authorCalls int
}

func (f *fakeLookup) Watch(context.Context, string) (ytmsearch.Watch, error) {
	return f.watch, f.watchErr
}

func (f *fakeLookup) YouTubeAuthor(context.Context, string) (string, error) {
	f.authorCalls++
	return f.author, f.authorErr
}

func (f *fakeLookup) SoundCloudAuthor(context.Context, string) (string, error) {
	f.authorCalls++
	return f.author, f.authorErr
}

const (
	youtubeURL    = "https://music.youtube.com/watch?v=dQw4w9WgXcQ"
	soundCloudURL = "https://soundcloud.com/daftpunk/one-more-time"
)

func TestClassifySource(t *testing.T) {
	boom := errors.New("rate limited")
	cases := []struct {
		name       string
		url        string
		lookup     fakeLookup
		want       grade.SourceKind
		wantErr    error
		wantAuthor bool
	}{
		{"no source url is the user's own upload", "", fakeLookup{}, grade.UserUpload, nil, false},
		{"youtube music album audio", youtubeURL, fakeLookup{watch: ytmsearch.Watch{Type: ytmsearch.VideoTypeATV}}, grade.ArtTrack, nil, false},
		{"youtube official video", youtubeURL, fakeLookup{watch: ytmsearch.Watch{Type: ytmsearch.VideoTypeOMV}}, grade.OfficialVideo, nil, false},
		{"youtube video of an unlisted type goes by its author", youtubeURL, fakeLookup{watch: ytmsearch.Watch{Type: "MUSIC_VIDEO_TYPE_OTHER"}, author: "lyrics hub"}, grade.FanUpload, nil, true},
		{"youtube watch with no type goes by its author", youtubeURL, fakeLookup{author: "lyrics hub"}, grade.FanUpload, nil, true},
		{"youtube upload by the artist", youtubeURL, fakeLookup{watch: ytmsearch.Watch{Type: ytmsearch.VideoTypeUGC}, author: "Daft Punk"}, grade.LabelUpload, nil, true},
		{"youtube upload by a fan", youtubeURL, fakeLookup{watch: ytmsearch.Watch{Type: ytmsearch.VideoTypeUGC}, author: "lyrics hub"}, grade.FanUpload, nil, true},
		{"youtube video outside the music catalog", youtubeURL, fakeLookup{watchErr: ytmsearch.ErrNoWatchTrack, author: "Daft Punk"}, grade.LabelUpload, nil, true},
		{"youtube watch lookup failing is unresolved", youtubeURL, fakeLookup{watchErr: boom}, "", boom, false},
		{"youtube author lookup failing is unresolved", youtubeURL, fakeLookup{watch: ytmsearch.Watch{Type: ytmsearch.VideoTypeUGC}, authorErr: boom}, "", boom, true},
		{"soundcloud upload by the artist", soundCloudURL, fakeLookup{author: "Daft Punk"}, grade.LabelUpload, nil, true},
		{"soundcloud upload by someone else", soundCloudURL, fakeLookup{author: "bootlegs"}, grade.FanUpload, nil, true},
		{"another host is unresolved", "https://example.com/song.mp3", fakeLookup{}, "", errUnsupportedSource, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lookup := tc.lookup
			got, err := classifySource(context.Background(), &lookup, "Daft Punk", tc.url)
			if got != tc.want || !errors.Is(err, tc.wantErr) || (lookup.authorCalls > 0) != tc.wantAuthor {
				t.Fatalf("got %q, %v (author lookups %d)", got, err, lookup.authorCalls)
			}
		})
	}
}

func TestPlanTrack_GradesFromTheStoredVerdictAndProvenance(t *testing.T) {
	cases := []struct {
		name string
		row  trackRow
		want grade.Grade
	}{
		{"fingerprint proven art track", trackRow{SourceURL: youtubeURL, Verdict: "hard", Provenance: "verified"}, grade.A},
		{"length only", trackRow{SourceURL: youtubeURL, Verdict: "unknown", Provenance: "corroborated"}, grade.C},
		{"no evidence stored", trackRow{SourceURL: youtubeURL, Provenance: "best_effort"}, grade.D},
		{"fingerprint says another version", trackRow{SourceURL: youtubeURL, Verdict: "other_version", Provenance: "verified"}, grade.F},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lookup := &fakeLookup{watch: ytmsearch.Watch{Type: ytmsearch.VideoTypeATV}}
			got, err := planTrack(context.Background(), lookup, tc.row, "run-1")
			if err != nil || got.Grade != tc.want || got.SourceKind != grade.ArtTrack || got.PipelineGeneration != 0 || got.BackfillRun != "run-1" {
				t.Fatalf("got %+v, %v", got, err)
			}
		})
	}
}
