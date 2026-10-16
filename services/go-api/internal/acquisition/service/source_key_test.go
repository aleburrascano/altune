package service

import (
	"strings"
	"testing"
)

func TestSourceKey_CollapsesTheSameYouTubeVideo(t *testing.T) {
	same := []string{
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ",
		"https://music.youtube.com/watch?v=dQw4w9WgXcQ",
		"https://m.youtube.com/watch?v=dQw4w9WgXcQ",
		"https://youtube.com/watch?v=dQw4w9WgXcQ&list=RDabc",
		"https://youtu.be/dQw4w9WgXcQ?si=xyz",
		"https://www.youtube.com/embed/dQw4w9WgXcQ",
		"https://www.youtube.com:443/watch?v=dQw4w9WgXcQ",
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

func TestSourceKey_RefusesAVideoIDFromALookalikeHost(t *testing.T) {
	imposters := []string{
		"https://notyoutube.com/watch?v=aaaaaaaaaaa",
		"https://evilyoutube.com/watch?v=aaaaaaaaaaa",
		"https://x.test/?u=youtube.com/watch?v=aaaaaaaaaaa",
		"https://www.youtube.com@evil.test/watch?v=aaaaaaaaaaa",
	}

	for _, raw := range imposters {
		got := sourceKey(raw)
		if strings.HasPrefix(got, "youtube:") {
			t.Errorf("sourceKey(%q) = %q, want a key of its own — a lookalike host must not take a video's identity", raw, got)
		}
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
	r := ReplaceState{ExcludeKeys: SourceKeys([]string{"https://music.youtube.com/watch?v=dQw4w9WgXcQ"})}

	if !r.excludes("https://www.youtube.com/watch?v=dQw4w9WgXcQ") {
		t.Error("a replace must not hand back the same video under its other URL")
	}
	if r.excludes("https://www.youtube.com/watch?v=aaaaaaaaaaa") {
		t.Error("a different video must not be excluded")
	}
}

func TestSourceKey_CollapsesEveryURLSpellingOfOneUpload(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want string
	}{
		{"youtube", []string{
			"https://www.youtube.com/watch?v=dQw4w9WgXcQ",
			"https://www.youtube.com/shorts/dQw4w9WgXcQ",
			"https://youtube.com/live/dQw4w9WgXcQ?feature=share",
			"https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ",
			"https://youtube-nocookie.com/embed/dQw4w9WgXcQ",
		}, "youtube:dQw4w9WgXcQ"},
		{"soundcloud", []string{
			"https://soundcloud.com/artist/song",
			"https://m.soundcloud.com/artist/song",
			"https://www.soundcloud.com/artist/song/",
		}, "soundcloud.com/artist/song"},
	}
	for _, tt := range tests {
		for _, raw := range tt.in {
			if got := sourceKey(raw); got != tt.want {
				t.Errorf("%s: sourceKey(%q) = %q, want %q", tt.name, raw, got, tt.want)
			}
		}
	}
}

func TestSourceKey_CollapsesYouTubeSpellingsWithCaseQueryAndFragment(t *testing.T) {
	same := []string{
		"https://WWW.YOUTUBE.COM/watch?v=dQw4w9WgXcQ",
		"https://WWW.YOUTUBE.COM/shorts/dQw4w9WgXcQ",
		"http://www.youtube.com/shorts/dQw4w9WgXcQ",
		"https://www.youtube.com/shorts/dQw4w9WgXcQ/",
		"https://www.youtube.com/shorts/dQw4w9WgXcQ?si=abc123",
		"https://www.youtube.com/shorts/dQw4w9WgXcQ#comments",
		"https://m.youtube.com/shorts/dQw4w9WgXcQ?feature=share",
		"https://youtube.com/shorts/dQw4w9WgXcQ?si=abc&t=10",
		"https://www.youtube.com/live/dQw4w9WgXcQ",
		"https://www.youtube.com/live/dQw4w9WgXcQ?si=abc&t=120",
		"https://www.youtube.com/live/dQw4w9WgXcQ/",
		"https://youtu.be/dQw4w9WgXcQ?t=42&si=xyz",
		"https://youtu.be/dQw4w9WgXcQ#t=42",
		"https://music.youtube.com/watch?v=dQw4w9WgXcQ&list=RDAMVM&si=x",
		"https://www.youtube.com/watch?list=PLabc&v=dQw4w9WgXcQ&t=5",
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ#t=30",
		"https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ?start=10&rel=0",
		"https://WWW.YOUTUBE-NOCOOKIE.COM/embed/dQw4w9WgXcQ",
		"https://www.youtube-nocookie.com:443/embed/dQw4w9WgXcQ",
		"http://youtube-nocookie.com/embed/dQw4w9WgXcQ#x",
	}

	want := "youtube:dQw4w9WgXcQ"
	for _, raw := range same {
		if got := sourceKey(raw); got != want {
			t.Errorf("sourceKey(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestSourceKey_KeepsDifferentVideosApartAcrossNewSpellings(t *testing.T) {
	pairs := [][2]string{
		{"https://www.youtube.com/shorts/dQw4w9WgXcQ", "https://www.youtube.com/shorts/aaaaaaaaaaa"},
		{"https://www.youtube.com/live/dQw4w9WgXcQ", "https://www.youtube.com/live/aaaaaaaaaaa"},
		{"https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ", "https://www.youtube-nocookie.com/embed/aaaaaaaaaaa"},
		{"https://www.youtube.com/shorts/dQw4w9WgXcQ", "https://www.youtube.com/shorts/DQW4W9WGXCQ"},
		{"https://m.soundcloud.com/artist/song", "https://m.soundcloud.com/artist/other"},
		{"https://m.soundcloud.com/artist/sets/song", "https://soundcloud.com/artist/song"},
	}
	for _, p := range pairs {
		if a, b := sourceKey(p[0]), sourceKey(p[1]); a == b {
			t.Errorf("sourceKey(%q) and sourceKey(%q) both = %q, want distinct keys", p[0], p[1], a)
		}
	}
}

func TestSourceKey_RefusesAVideoIDFromALookalikeOfTheNewHostsAndPaths(t *testing.T) {
	imposters := []string{
		"https://youtube.com.evil.com/shorts/dQw4w9WgXcQ",
		"https://www.youtube.com.evil.com/live/dQw4w9WgXcQ",
		"https://evilyoutube.com/shorts/dQw4w9WgXcQ",
		"https://notyoutube-nocookie.com/embed/dQw4w9WgXcQ",
		"https://youtube-nocookie.com.evil.com/embed/dQw4w9WgXcQ",
		"https://www.youtube-nocookie.evil.com/embed/dQw4w9WgXcQ",
		"https://youtu.be.evil.com/dQw4w9WgXcQ",
		"https://m.youtube.com.evil.com/shorts/dQw4w9WgXcQ",
		"https://music.youtube.com.evil.com/watch?v=dQw4w9WgXcQ",
		"https://www.youtube.com@evil.test/shorts/dQw4w9WgXcQ",
		"https://www.youtube-nocookie.com@evil.test/embed/dQw4w9WgXcQ",
		"https://evil.test/www.youtube.com/shorts/dQw4w9WgXcQ",
		"https://evil.test/shorts/dQw4w9WgXcQ",
		"https://evil.test/embed/dQw4w9WgXcQ?u=https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ",
		"https://xn--youtube-ix0d.com/shorts/dQw4w9WgXcQ",
		"https://yоutube.com/shorts/dQw4w9WgXcQ",
		"https://www.youtube.com.:443@evil.test/live/dQw4w9WgXcQ",
	}
	for _, raw := range imposters {
		if got := sourceKey(raw); strings.HasPrefix(got, "youtube:") {
			t.Errorf("sourceKey(%q) = %q, want a key that is not a YouTube video key", raw, got)
		}
	}
}

func TestSourceKey_KeepsALookalikeSoundCloudHostOffTheSoundCloudKey(t *testing.T) {
	want := "soundcloud.com/artist/song"
	imposters := []string{
		"https://m.soundcloud.com.attacker.net/artist/song",
		"https://soundcloud.com.attacker.net/artist/song",
		"https://evilsoundcloud.com/artist/song",
		"https://m.evilsoundcloud.com/artist/song",
		"https://m.soundcloud.com@attacker.net/artist/song",
		"https://attacker.net/soundcloud.com/artist/song",
		"https://attacker.net/m.soundcloud.com/artist/song",
		"https://sоundcloud.com/artist/song",
	}
	for _, raw := range imposters {
		got := sourceKey(raw)
		if got == want || strings.HasPrefix(got, "soundcloud.com/") {
			t.Errorf("sourceKey(%q) = %q, want a key that is not a soundcloud.com key", raw, got)
		}
	}
}

func TestSourceKey_CollapsesMobileSoundCloudSpellingsOfOneUpload(t *testing.T) {
	tests := []struct {
		in   []string
		want string
	}{
		{[]string{
			"https://m.soundcloud.com/Artist/Song",
			"https://m.soundcloud.com/artist/song/",
			"http://m.soundcloud.com/artist/song",
			"https://m.soundcloud.com/artist/song?si=abc&utm_source=clipboard",
			"https://SoundCloud.com/ARTIST/SONG",
		}, "soundcloud.com/artist/song"},
		{[]string{
			"https://soundcloud.com/artist/sets/album",
			"https://m.soundcloud.com/artist/sets/album",
			"https://m.soundcloud.com/Artist/Sets/Album/",
			"https://www.soundcloud.com/artist/sets/album?si=x",
		}, "soundcloud.com/artist/sets/album"},
	}
	for _, tt := range tests {
		for _, raw := range tt.in {
			if got := sourceKey(raw); got != tt.want {
				t.Errorf("sourceKey(%q) = %q, want %q", raw, got, tt.want)
			}
		}
	}
}

func TestSourceKey_GivesNoSharedYouTubeKeyToEmptyOrMalformedInput(t *testing.T) {
	inputs := []string{
		"",
		"   ",
		"not a url",
		"://",
		"https://",
		"https:///shorts/dQw4w9WgXcQ",
		"https://www.youtube.com/shorts/",
		"https://www.youtube.com/shorts",
		"https://www.youtube.com/live/",
		"https://www.youtube.com/live",
		"https://www.youtube-nocookie.com/embed/",
		"https://www.youtube-nocookie.com/",
		"https://youtu.be/",
		"https://www.youtube.com/@artist/live",
		"https://www.youtube.com/shorts/%00%00",
	}
	for _, raw := range inputs {
		if got := sourceKey(raw); got == "youtube:" || got == "youtube:live" || got == "youtube:shorts" || got == "youtube:embed" {
			t.Errorf("sourceKey(%q) = %q, want no YouTube key without a video id", raw, got)
		}
	}
	if got := sourceKey(""); got != "" {
		t.Errorf("sourceKey(\"\") = %q, want empty", got)
	}
}

func TestSourceKey_CollapsesAnUppercaseMobileSoundCloudHost(t *testing.T) {
	tests := []struct{ in, want string }{
		{"https://M.SOUNDCLOUD.COM/Artist/Song", "soundcloud.com/artist/song"},
		{"https://M.SoundCloud.com/Artist/Sets/Album/", "soundcloud.com/artist/sets/album"},
	}
	for _, tt := range tests {
		if got := sourceKey(tt.in); got != tt.want {
			t.Errorf("sourceKey(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
