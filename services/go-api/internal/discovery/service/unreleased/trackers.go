package unreleased

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"context"
	"errors"
	"log/slog"
	"slices"
)

func WithTrackers(trackers ports.TrackerSongs) Option {
	return func(s *Service) { s.trackers = trackers }
}

type sourceResult struct {
	songs    []domain.UnreleasedSong
	complete bool
	err      error
	used     bool
}

func (r sourceResult) answered() bool { return r.used && r.err == nil }

func (s *Service) trackerResult(ctx context.Context, artistName string, catalogTitles []string) sourceResult {
	if s.trackers == nil {
		return sourceResult{complete: true}
	}
	songs, err := s.trackers.ArtistSongs(ctx, artistName)
	if errors.Is(err, ports.ErrTrackerPartial) {
		slog.WarnContext(ctx, "trackers.partial", "error", err)
		return sourceResult{songs: withoutReleasedVersions(songs, catalogTitles), used: true}
	}
	if err != nil {
		slog.WarnContext(ctx, "trackers.fetch_failed", "error", err)
		return sourceResult{used: true, err: err}
	}
	return sourceResult{songs: withoutReleasedVersions(songs, catalogTitles), complete: true, used: true}
}

func (s *Service) geniusResult(ctx context.Context, artistName string, catalogTitles []string) sourceResult {
	if s.genius == nil {
		return sourceResult{}
	}
	artistID, found, err := s.genius.FindArtistID(ctx, artistName, catalogTitles)
	if err != nil {
		slog.WarnContext(ctx, "unreleased.genius_failed", "error", err)
		return sourceResult{used: true, err: err}
	}
	if !found {
		return sourceResult{songs: []domain.UnreleasedSong{}, complete: true, used: true}
	}
	songs, complete, err := s.unreleasedSongs(ctx, artistID, catalogTitles)
	return sourceResult{songs: songs, complete: complete, used: true, err: err}
}

func combineSources(genius, trackers sourceResult) ([]domain.UnreleasedSong, bool, error) {
	if !genius.answered() && !trackers.answered() {
		return nil, false, errors.Join(genius.err, trackers.err)
	}
	complete := genius.complete && trackers.complete && genius.err == nil && trackers.err == nil
	return mergeTrackerSongs(genius.songs, trackers.songs), complete, nil
}

func mergeTrackerSongs(genius, trackers []domain.UnreleasedSong) []domain.UnreleasedSong {
	merged := append([]domain.UnreleasedSong{}, genius...)
	at := make(map[string]int, len(merged))
	for i, song := range merged {
		at[titleKey(song.Title)] = i
	}
	for _, tracked := range trackers {
		key := titleKey(tracked.Title)
		if i, ok := at[key]; ok {
			merged[i] = withTrackerFile(merged[i], tracked)
			continue
		}
		at[key] = len(merged)
		merged = append(merged, tracked)
	}
	return merged
}

func withTrackerFile(song, tracked domain.UnreleasedSong) domain.UnreleasedSong {
	if tracked.Quality != "" {
		song.Quality = tracked.Quality
	}
	song.TrackerKey = tracked.TrackerKey
	song.OffStreaming = song.OffStreaming || tracked.OffStreaming
	song.AudioLinks = appendNewLinks(slices.Clone(song.AudioLinks), tracked.AudioLinks)
	if len(song.AudioLinks) > 0 {
		hasMedia := true
		song.HasMedia = &hasMedia
	}
	if song.OpenURL == "" {
		song.OpenURL, song.OpenHost = tracked.OpenURL, tracked.OpenHost
	}
	return song
}

func withoutReleasedVersions(songs []domain.UnreleasedSong, catalogTitles []string) []domain.UnreleasedSong {
	released := make(map[string]struct{}, len(catalogTitles))
	for _, title := range catalogTitles {
		released[titleKey(title)] = struct{}{}
	}
	kept := make([]domain.UnreleasedSong, 0, len(songs))
	for _, song := range songs {
		if len(song.Versions) == 0 {
			if _, isReleased := released[titleKey(song.Title)]; !isReleased {
				kept = append(kept, song)
			}
			continue
		}
		if song, ok := withoutReleasedCopies(song, released); ok {
			kept = append(kept, song)
		}
	}
	return kept
}

func withoutReleasedCopies(song domain.UnreleasedSong, released map[string]struct{}) (domain.UnreleasedSong, bool) {
	versions := make([]domain.UnreleasedVersion, 0, len(song.Versions))
	for _, version := range song.Versions {
		_, isReleased := released[titleKey(version.Title)]
		if version.Label == "" && isReleased {
			continue
		}
		versions = append(versions, version)
	}
	if len(versions) == 0 {
		return song, false
	}
	if len(versions) != len(song.Versions) {
		song.Versions = versions
		song.ApplyBestVersion()
	}
	return song, true
}

func appendNewLinks(have, more []domain.AudioLink) []domain.AudioLink {
	for _, link := range more {
		if !slices.Contains(have, link) {
			have = append(have, link)
		}
	}
	return have
}
