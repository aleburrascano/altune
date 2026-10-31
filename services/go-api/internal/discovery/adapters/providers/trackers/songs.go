package trackers

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/textnorm"
	"altune/go-api/internal/shared/versiontag"
	"fmt"
	"slices"
	"strings"
	"unicode"
)

var qualityRanks = map[string]int{"lossless": 5, "cd quality": 4, "high quality": 3, "recording": 2, "low quality": 1}

func qualityRank(quality string) int {
	return qualityRanks[strings.ToLower(strings.TrimSpace(quality))]
}

func versionNumber(label string) int {
	var n int
	if _, err := fmt.Sscanf(label, "V%d", &n); err != nil {
		return 0
	}
	return n
}

func isBetterVersion(candidate, current domain.UnreleasedVersion) bool {
	if cr, br := qualityRank(candidate.Quality), qualityRank(current.Quality); cr != br {
		return cr > br
	}
	return versionNumber(candidate.Label) > versionNumber(current.Label)
}

func songsFromRows(artistName string, rows []trackerRow) []domain.UnreleasedSong {
	songs := make([]domain.UnreleasedSong, 0, len(rows))
	byKey := map[string]int{}
	var released []trackerRow
	for _, row := range rows {
		if row.offStreaming {
			released = append(released, row)
			continue
		}
		base, label := versiontag.BaseTitle(row.title)
		key := trackerKey(artistName, base)
		at, seen := byKey[key]
		if !seen {
			at = len(songs)
			byKey[key] = at
			songs = append(songs, domain.UnreleasedSong{Title: base, TrackerKey: key})
		}
		songs[at].Versions = append(songs[at].Versions, domain.UnreleasedVersion{
			Title: row.title, Label: label, Quality: row.quality,
			AudioLinks: row.links, OpenURL: row.openURL, OpenHost: row.openHost,
		})
	}
	for at := range songs {
		applyBestVersion(&songs[at])
	}
	for _, row := range released {
		songs = addReleasedRow(songs, byKey, artistName, row)
	}
	return songs
}

func applyBestVersion(song *domain.UnreleasedSong) {
	best := song.Versions[0]
	for _, version := range song.Versions[1:] {
		if isBetterVersion(version, best) {
			best = version
		}
	}
	hasMedia := len(best.AudioLinks) > 0
	song.Quality, song.AudioLinks, song.HasMedia = best.Quality, slices.Clone(best.AudioLinks), &hasMedia
	song.OpenURL, song.OpenHost = best.OpenURL, best.OpenHost
}

func addReleasedRow(songs []domain.UnreleasedSong, byKey map[string]int, artistName string, row trackerRow) []domain.UnreleasedSong {
	base, _ := versiontag.BaseTitle(row.title)
	key := trackerKey(artistName, base)
	hasMedia := len(row.links) > 0
	at, dup := byKey[key]
	if !dup {
		byKey[key] = len(songs)
		return append(songs, domain.UnreleasedSong{
			Title: base, Quality: row.quality, TrackerKey: key, HasMedia: &hasMedia, AudioLinks: row.links,
			OffStreaming: true, OpenURL: row.openURL, OpenHost: row.openHost,
		})
	}
	songs[at].AudioLinks = appendNewLinks(songs[at].AudioLinks, row.links)
	songs[at].OffStreaming = true
	if songs[at].OpenURL == "" {
		songs[at].OpenURL, songs[at].OpenHost = row.openURL, row.openHost
	}
	hasMedia = len(songs[at].AudioLinks) > 0
	songs[at].HasMedia = &hasMedia
	return songs
}

func appendNewLinks(have, more []domain.AudioLink) []domain.AudioLink {
	for _, link := range more {
		if !slices.Contains(have, link) {
			have = append(have, link)
		}
	}
	return have
}

func trackerKey(artistName, title string) string {
	return slug(artistName) + ":" + slug(title)
}

func slug(s string) string {
	var b strings.Builder
	pendingDash := false
	for _, r := range textnorm.NormalizeForIdentity(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if pendingDash && b.Len() > 0 {
				b.WriteByte('-')
			}
			pendingDash = false
			b.WriteRune(r)
			continue
		}
		pendingDash = true
	}
	return b.String()
}
