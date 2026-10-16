package unreleased

import (
	"altune/go-api/internal/discovery/domain"
	"regexp"
	"strings"
)

var translationMarkers = []string{
	"çeviri", "translation", "traducción", "tradução", "traduction",
	"übersetzung", "traduzione", "перевод", "翻訳", "번역",
}

var bracketedTagRe = regexp.MustCompile(`[(\[]([^)\]]*)[)\]]`)

func isTranslationTitle(title string) bool {
	for _, match := range bracketedTagRe.FindAllStringSubmatch(strings.ToLower(title), -1) {
		for _, marker := range translationMarkers {
			if strings.Contains(match[1], marker) {
				return true
			}
		}
	}
	return false
}

func withoutTranslations(songs []domain.UnreleasedSong) []domain.UnreleasedSong {
	kept := make([]domain.UnreleasedSong, 0, len(songs))
	for _, song := range songs {
		if !isTranslationTitle(song.Title) {
			kept = append(kept, song)
		}
	}
	return kept
}
