package domain

// DeezerLyrics is the Deezer-derived lyrics surface for one track: the full
// plain text, the time-synced (LRC-style) lines when available, the songwriter
// credits, and the publishing copyright line. Immutable value object — a live
// read surface fetched on detail-open, never persisted.
//
// Lyrics are the one metadata axis no other audited provider carries
// (MusicBrainz, Discogs, and Last.fm have none). Sourced from Deezer's internal
// pipe.deezer.com GraphQL (the anonymous-JWT path), separate from the public-API
// DeezerEnrichment surface. Introduced by docs/providers/deezer.md (cap 6).
//
// Availability is per-track and region-dependent: a track can carry Plain but no
// SyncedLines, or nothing at all (the graceful path returns EmptyDeezerLyrics).
type DeezerLyrics struct {
	Plain       string            // full plain-text lyrics ("" when unavailable)
	SyncedLines []SyncedLyricLine // time-synced lines; empty when only plain text exists
	Writers     []string          // songwriter credits (split from the comma-joined source)
	Copyright   string            // publishing copyright line
}

// SyncedLyricLine is one time-synced lyric line: the LRC-style timecode, the
// text, and the start offset + duration in milliseconds for player scrubbing.
type SyncedLyricLine struct {
	Timecode     string // LRC-style "[mm:ss.xx]" marker (Deezer lrcTimestamp)
	Line         string // the line text
	Milliseconds int64  // start offset from track start, in ms
	Duration     int64  // line duration, in ms
}

// EmptyDeezerLyrics returns a zero-value lyrics surface with non-nil slices, so
// the wire mapping never emits a null list. The graceful degradation path
// (no track id, no lyrics for this region, Deezer error) returns this.
func EmptyDeezerLyrics() DeezerLyrics {
	return DeezerLyrics{SyncedLines: []SyncedLyricLine{}, Writers: []string{}}
}

// IsZero reports whether there is nothing worth rendering — used to decide there
// is no lyrics section to show, and to negative-cache a definitive miss.
func (l DeezerLyrics) IsZero() bool {
	return l.Plain == "" && len(l.SyncedLines) == 0
}
