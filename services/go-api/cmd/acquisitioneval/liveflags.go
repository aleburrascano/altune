package main

import (
	"altune/go-api/internal/acquisition/adapters/ytmsearch"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

const (
	durationDeltaFlagSeconds = 8.0
	topSongsConsidered       = 5
	minSpacelessArtistLength = 3
)

var badQualifiers = []string{
	"music video", "official video", "video", "live", "remix", "clean",
	"radio edit", "edited", "slowed", "sped up", "spedup", "reverb", "reaction", "instrumental",
	"karaoke", "cover", "nightcore", "8d", "bass boosted", "lyrics", "lyric", "acapella", "a cappella",
	"snippet", "leak", "extended", "loop", "type beat", "chopped", "screwed", "mashup",
	"performance", "interview", "tiktok",
}

var (
	videoQualifiers      = map[string]bool{"video": true, "music video": true, "official video": true}
	cleanTitleQualifiers = map[string]bool{"clean": true, "edited": true, "radio edit": true}
)

var (
	nonAlphanumericRun = regexp.MustCompile(`[^a-z0-9]+`)
	featuringParens    = regexp.MustCompile(`(?i)\s*[\(\[](feat|ft|with|prod)\.?[^\)\]]*[\)\]]`)
	featuringTail      = regexp.MustCompile(`(?i)\s+(feat|ft)\.?\s.*$`)
	releaseSuffix      = regexp.MustCompile(`(?i)\s*[\(\[](bonus( track)?|explicit|album version|remaster(ed)?( \d{4})?|\d{4} remaster(ed)?|original mix)[\)\]]`)
	artistSeparators   = regexp.MustCompile(`(?i),|&| x | and |\bfeat\b\.?|\bft\b\.?|\bwith\b`)
)

type youtubeSource struct {
	VideoID string
	Title   string
	Author  string
	Type    ytmsearch.VideoType
	Songs   []ytmsearch.Song
}

func youtubeFlags(track liveTrack, src youtubeSource) []string {
	qualifiers := unrequestedQualifiers(src.Title, track.Title)
	return append([]string{}, slices.Concat(
		videoTypeFlags(src.Type),
		flagIf(hasAny(qualifiers, videoQualifiers), "video_title"),
		flagIf(isFanReuploadChannel(src.Author, track.Artist), "fan_reupload_channel"),
		flagIf(hasCleanEditWithExplicitTwin(track, src), "clean_edit(explicit exists)"),
		qualifierFlags(qualifiers),
		flagIf(hasAny(qualifiers, cleanTitleQualifiers), "clean_edit(title)"),
		durationDeltaFlags(track.durationDeltaSeconds()),
	)...)
}

func flagIf(condition bool, flag string) []string {
	if condition {
		return []string{flag}
	}
	return nil
}

func videoTypeFlags(videoType ytmsearch.VideoType) []string {
	switch videoType {
	case ytmsearch.VideoTypeOMV:
		return []string{"music_video(OMV)"}
	case ytmsearch.VideoTypeUGC:
		return []string{"user_upload(UGC)"}
	}
	return nil
}

func qualifierFlags(qualifiers []string) []string {
	var flags []string
	for _, q := range qualifiers {
		if !videoQualifiers[q] {
			flags = append(flags, "qualifier:"+q)
		}
	}
	return flags
}

func durationDeltaFlags(delta float64) []string {
	return flagIf(math.Abs(delta) > durationDeltaFlagSeconds, fmt.Sprintf("duration_delta:%.0fs", delta))
}

func hasAny(qualifiers []string, wanted map[string]bool) bool {
	for _, q := range qualifiers {
		if wanted[q] {
			return true
		}
	}
	return false
}

func isFanReuploadChannel(author, artist string) bool {
	if author == "" {
		return false
	}
	normalized := normText(author)
	isTopic := strings.HasSuffix(normalized, " topic")
	isVevo := strings.HasSuffix(normalized, "vevo")
	return !isTopic && !isVevo && !artistMatches(author, artist)
}

func hasCleanEditWithExplicitTwin(track liveTrack, src youtubeSource) bool {
	songs := src.Songs[:min(topSongsConsidered, len(src.Songs))]
	stored := -1
	for i, song := range songs {
		if song.VideoID == src.VideoID {
			stored = i
			break
		}
	}
	if stored < 0 || songs[stored].Explicit {
		return false
	}
	for _, song := range songs {
		if song.Explicit && titleMatches(song.Title, track.Title) && artistMatches(strings.Join(song.Artists, ", "), track.Artist) {
			return true
		}
	}
	return false
}

func normText(s string) string {
	var ascii strings.Builder
	for _, r := range norm.NFKD.String(s) {
		if r < utf8.RuneSelf {
			ascii.WriteRune(unicode.ToLower(r))
		}
	}
	replaced := strings.NewReplacer("&", " and ", "$", "s", "'", "", "`", "").Replace(ascii.String())
	return strings.Join(strings.Fields(nonAlphanumericRun.ReplaceAllString(replaced, " ")), " ")
}

func unrequestedQualifiers(candidateTitle, trackTitle string) []string {
	candidate, track := " "+normText(candidateTitle)+" ", " "+normText(trackTitle)+" "
	var found []string
	for _, q := range badQualifiers {
		padded := " " + normText(q) + " "
		if strings.Contains(candidate, padded) && !strings.Contains(track, padded) {
			found = append(found, q)
		}
	}
	return found
}

func coreTitle(title string) string {
	title = featuringParens.ReplaceAllString(title, "")
	title = featuringTail.ReplaceAllString(title, "")
	title = releaseSuffix.ReplaceAllString(title, "")
	return normText(title)
}

func titleMatches(candidateTitle, trackTitle string) bool {
	candidate, track := coreTitle(candidateTitle), coreTitle(trackTitle)
	if track == "" {
		return false
	}
	return track == candidate || strings.Contains(" "+candidate+" ", " "+track+" ")
}

func firstArtistToken(artist string) string {
	for _, part := range artistSeparators.Split(artist, -1) {
		if token := normText(part); token != "" {
			return token
		}
	}
	return ""
}

func artistMatches(candidateText, artist string) bool {
	token := firstArtistToken(artist)
	if token == "" {
		return false
	}
	candidate := " " + normText(candidateText) + " "
	if strings.Contains(candidate, " "+token+" ") {
		return true
	}
	spaceless := strings.ReplaceAll(token, " ", "")
	return len(spaceless) >= minSpacelessArtistLength && strings.Contains(strings.ReplaceAll(candidate, " ", ""), spaceless)
}
