package service

import (
	"altune/go-api/internal/shared/textnorm"
	"crypto/sha256"
	"encoding/hex"
	"path"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

func stagedReplaceRef(canonical, attemptID string) string {
	ext := path.Ext(canonical)
	return strings.TrimSuffix(canonical, ext) + ".replace-" + attemptID + ext
}

func BuildAudioRef(track TrackRef, tempPath string) string {
	return buildAudioRef(track, tempPath, normalizePathComponent)
}

func BuildLegacyAudioRef(track TrackRef, tempPath string) string {
	return buildAudioRef(track, tempPath, sanitizePathComponent)
}

func BuildLegacyAudioRefUncapped(track TrackRef, tempPath string) string {
	return buildAudioRef(track, tempPath, sanitizePathComponentUncapped)
}

func buildAudioRef(track TrackRef, tempPath string, segment func(string) string) string {
	artist := segment(track.Artist)
	album := track.Album
	if album == "" {
		album = "Unknown Album"
	}
	album = segment(album)
	title := segment(track.Title)

	ext := filepath.Ext(tempPath)
	if ext == "" {
		ext = ".mp3"
	}
	return strings.Join([]string{track.UserID, artist, album, title + ext}, "/")
}

func normalizePathComponent(s string) string {
	normalized := sanitizePathComponent(textnorm.NormalizeForIdentity(s))
	if normalized != "Unknown" || strings.TrimSpace(s) == "" {
		return normalized
	}
	sum := sha256.Sum256([]byte(s))
	return "Unknown-" + hex.EncodeToString(sum[:4])
}

func sanitizePathComponent(s string) string {
	return capSegmentBytes(sanitizePathComponentUncapped(s))
}

func sanitizePathComponentUncapped(s string) string {
	if s == "" {
		return "Unknown"
	}
	forbidden := `<>:"/\|?*;`
	var b strings.Builder
	for _, r := range s {
		if !strings.ContainsRune(forbidden, r) && (unicode.IsSpace(r) || !unicode.IsControl(r)) {
			b.WriteRune(r)
		}
	}
	result := strings.Join(strings.Fields(b.String()), " ")
	if result == "" {
		return "Unknown"
	}
	if strings.Trim(result, ".") == "" {
		return "Unknown"
	}
	return result
}

const (
	maxSegmentBytes = 120
	segmentHashLen  = 8
)

func capSegmentBytes(s string) string {
	if len(s) <= maxSegmentBytes {
		return s
	}
	sum := sha256.Sum256([]byte(s))
	suffix := "-" + hex.EncodeToString(sum[:])[:segmentHashLen]
	cut := maxSegmentBytes - len(suffix)
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + suffix
}
