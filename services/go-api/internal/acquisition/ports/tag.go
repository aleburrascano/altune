package ports

import "context"

type TrackTags struct {
	Title       string
	Artist      string
	Album       string
	AlbumArtist string
	Genre       string
	Year        int
	TrackNumber int
}

type AudioTagger interface {
	Tag(ctx context.Context, filePath string, tags TrackTags) error
}
