package domain

import (
	"fmt"
	"slices"
	"strings"
)

var qualityRanks = map[string]int{"lossless": 5, "cd quality": 4, "high quality": 3, "recording": 2, "low quality": 1}

func qualityRank(quality string) int {
	return qualityRanks[strings.ToLower(strings.TrimSpace(quality))]
}

func versionNumber(label string) [2]int {
	var major, minor int
	if n, _ := fmt.Sscanf(label, "V%d.%d", &major, &minor); n == 0 {
		return [2]int{}
	}
	return [2]int{major, minor}
}

func laterVersion(a, b [2]int) bool {
	if a[0] != b[0] {
		return a[0] > b[0]
	}
	return a[1] > b[1]
}

func isBetterVersion(candidate, current UnreleasedVersion) bool {
	if cr, br := qualityRank(candidate.Quality), qualityRank(current.Quality); cr != br {
		return cr > br
	}
	return laterVersion(versionNumber(candidate.Label), versionNumber(current.Label))
}

func (s UnreleasedSong) BestVersion() UnreleasedVersion {
	best := s.Versions[0]
	for _, version := range s.Versions[1:] {
		if isBetterVersion(version, best) {
			best = version
		}
	}
	return best
}

func (s *UnreleasedSong) ApplyBestVersion() {
	best := s.BestVersion()
	hasMedia := len(best.AudioLinks) > 0
	s.Quality, s.AudioLinks, s.HasMedia = best.Quality, slices.Clone(best.AudioLinks), &hasMedia
	s.OpenURL, s.OpenHost = best.OpenURL, best.OpenHost
}
