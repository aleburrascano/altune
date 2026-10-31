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

func versionNumber(label string) int {
	var n int
	if _, err := fmt.Sscanf(label, "V%d", &n); err != nil {
		return 0
	}
	return n
}

func isBetterVersion(candidate, current UnreleasedVersion) bool {
	if cr, br := qualityRank(candidate.Quality), qualityRank(current.Quality); cr != br {
		return cr > br
	}
	return versionNumber(candidate.Label) > versionNumber(current.Label)
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
