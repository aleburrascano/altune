package unreleased

import (
	"altune/go-api/internal/discovery/domain"
	"strings"
)

var nonTrackSuffixes = []string{"setlist", "set list", "tracklist", "track list", "tour dates", "tour outfits"}

func isNonTrackTitle(title string) bool {
	stripped := strings.ToLower(strings.TrimSpace(bracketedTagRe.ReplaceAllString(title, " ")))
	for _, suffix := range nonTrackSuffixes {
		if strings.HasSuffix(stripped, suffix) {
			return true
		}
	}
	return false
}

func isNonTrackPage(song domain.UnreleasedSong) bool {
	bare := song.GeniusID != 0 && song.TrackerKey == "" && len(song.AudioLinks) == 0 && song.OpenURL == ""
	return bare && isNonTrackTitle(song.Title)
}

func withoutNonTrackPages(songs []domain.UnreleasedSong) ([]domain.UnreleasedSong, int) {
	kept := make([]domain.UnreleasedSong, 0, len(songs))
	for _, song := range songs {
		if !isNonTrackPage(song) {
			kept = append(kept, song)
		}
	}
	return kept, len(songs) - len(kept)
}
