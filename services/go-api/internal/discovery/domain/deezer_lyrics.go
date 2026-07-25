package domain

type DeezerLyrics struct {
	Plain       string
	SyncedLines []SyncedLyricLine
	Writers     []string
	Copyright   string
}

type SyncedLyricLine struct {
	Timecode     string
	Line         string
	Milliseconds int64
	Duration     int64
}

func EmptyDeezerLyrics() DeezerLyrics {
	return DeezerLyrics{SyncedLines: []SyncedLyricLine{}, Writers: []string{}}
}

func (l DeezerLyrics) IsZero() bool {
	return l.Plain == "" && len(l.SyncedLines) == 0
}
