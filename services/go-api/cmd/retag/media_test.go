package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fakeTool(t *testing.T, name, body string) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(binary, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return binary
}

func TestFfmpegTools_ReadTagsParsesFfprobeJSON(t *testing.T) {
	ffprobe := fakeTool(t, "ffprobe", `echo '{"format":{"tags":{"title":"Gorah","date":"2021"}}}'`)

	got, err := ffmpegTools{ffprobe: ffprobe}.ReadTags(context.Background(), "x.m4a")
	if err != nil || got.Title != "Gorah" || got.Year != 2021 {
		t.Fatalf("ReadTags = %+v, %v", got, err)
	}
}

func TestFfmpegTools_ReadTagsReportsFfprobeFailure(t *testing.T) {
	ffprobe := fakeTool(t, "ffprobe", `echo 'moov atom not found' >&2; exit 1`)

	_, err := ffmpegTools{ffprobe: ffprobe}.ReadTags(context.Background(), "x.m4a")
	if err == nil || !strings.Contains(err.Error(), "moov atom not found") {
		t.Fatalf("ReadTags error = %v", err)
	}
}

func TestFfmpegTools_AudioDigestReadsTheMD5Line(t *testing.T) {
	ffmpeg := fakeTool(t, "ffmpeg", `echo 'MD5=0123abcd'`)

	got, err := ffmpegTools{ffmpeg: ffmpeg}.AudioDigest(context.Background(), "x.m4a")
	if err != nil || got != "0123abcd" {
		t.Fatalf("AudioDigest = %q, %v", got, err)
	}
}

func TestFfmpegTools_AudioDigestRejectsUnexpectedOutput(t *testing.T) {
	ffmpeg := fakeTool(t, "ffmpeg", `echo 'garbage'`)

	if _, err := (ffmpegTools{ffmpeg: ffmpeg}).AudioDigest(context.Background(), "x.m4a"); err == nil {
		t.Fatal("expected an error")
	}
}
