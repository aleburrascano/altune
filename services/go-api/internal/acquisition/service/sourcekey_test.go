package service

import "testing"

func TestSourceKey_CollapsesTheSameYouTubeVideo(t *testing.T) {
	same := []string{
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ",
		"https://music.youtube.com/watch?v=dQw4w9WgXcQ",
		"https://youtube.com/watch?v=dQw4w9WgXcQ&list=RDabc",
		"https://youtu.be/dQw4w9WgXcQ",
		"https://www.youtube.com/embed/dQw4w9WgXcQ",
	}

	want := "youtube:dQw4w9WgXcQ"
	for _, raw := range same {
		if got := sourceKey(raw); got != want {
			t.Errorf("sourceKey(%q) = %q, want %q — the same video under two spellings must exclude as one", raw, got, want)
		}
	}
}

func TestSourceKey_DistinguishesDifferentVideos(t *testing.T) {
	a := sourceKey("https://www.youtube.com/watch?v=dQw4w9WgXcQ")
	b := sourceKey("https://www.youtube.com/watch?v=aaaaaaaaaaa")
	if a == b {
		t.Errorf("distinct videos collapsed to %q", a)
	}
}

func TestSourceKey_NormalizesNonYouTubeURLs(t *testing.T) {
	tests := []struct{ in, want string }{
		{"https://soundcloud.com/artist/song", "soundcloud.com/artist/song"},
		{"http://www.soundcloud.com/artist/song/", "soundcloud.com/artist/song"},
		{"https://soundcloud.com/Artist/Song?in=x", "soundcloud.com/artist/song"},
		{"yt:master", "yt:master"},
	}
	for _, tt := range tests {
		if got := sourceKey(tt.in); got != tt.want {
			t.Errorf("sourceKey(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSourceKeys_DedupesAndDropsEmpty(t *testing.T) {
	got := SourceKeys([]string{
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ",
		"https://music.youtube.com/watch?v=dQw4w9WgXcQ",
		"",
		"https://soundcloud.com/a/b",
	})
	if len(got) != 2 {
		t.Fatalf("keys = %v, want the two distinct recordings", got)
	}
}

func TestExcludes_MatchesAcrossURLSpellings(t *testing.T) {
	ac := &AcquisitionContext{ExcludeKeys: SourceKeys([]string{"https://music.youtube.com/watch?v=dQw4w9WgXcQ"})}

	if !ac.excludes("https://www.youtube.com/watch?v=dQw4w9WgXcQ") {
		t.Error("a replace must not hand back the same video under its other URL")
	}
	if ac.excludes("https://www.youtube.com/watch?v=aaaaaaaaaaa") {
		t.Error("a different video must not be excluded")
	}
}
