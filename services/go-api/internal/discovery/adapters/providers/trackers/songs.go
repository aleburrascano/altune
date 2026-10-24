package trackers

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/textnorm"
	"slices"
	"strings"
	"unicode"
)

const pixeldrainScheme = "https://"

func songsFromRows(artistName string, rows []trackerRow) []domain.UnreleasedSong {
	songs := make([]domain.UnreleasedSong, 0, len(rows))
	byKey := map[string]int{}
	for _, row := range rows {
		song := songFromRow(artistName, row)
		if at, dup := byKey[song.TrackerKey]; dup {
			songs[at].AudioLinks = appendNewLinks(songs[at].AudioLinks, song.AudioLinks)
			continue
		}
		byKey[song.TrackerKey] = len(songs)
		songs = append(songs, song)
	}
	return songs
}

func songFromRow(artistName string, row trackerRow) domain.UnreleasedSong {
	hasMedia := true
	links := make([]domain.AudioLink, len(row.pixeldrains))
	for i, id := range row.pixeldrains {
		links[i] = domain.AudioLink{Provider: domain.AudioProviderPixeldrain, URL: pixeldrainScheme + pixeldrainHost + "/u/" + id}
	}
	return domain.UnreleasedSong{
		Title:      row.title,
		Quality:    row.quality,
		TrackerKey: trackerKey(artistName, row.title),
		HasMedia:   &hasMedia,
		AudioLinks: links,
	}
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
