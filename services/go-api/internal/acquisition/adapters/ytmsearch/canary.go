package ytmsearch

import (
	"context"
	"errors"
	"fmt"
)

const (
	canarySearchQuery = "Never Gonna Give You Up Rick Astley"
	canaryVideoID     = "dQw4w9WgXcQ"
)

func (c *Client) Canary(ctx context.Context) error {
	songs, err := c.SearchSongs(ctx, canarySearchQuery)
	if err != nil {
		return fmt.Errorf("search: %w", err)
	}
	if !hasATVSong(songs) {
		return errors.New("search: no ATV song with a video id")
	}
	watch, err := c.Watch(ctx, canaryVideoID)
	if err != nil {
		return fmt.Errorf("next: %w", err)
	}
	if watch.Title == "" {
		return errors.New("next: empty title")
	}
	return nil
}

func hasATVSong(songs []Song) bool {
	for _, s := range songs {
		if s.Type == VideoTypeATV && s.VideoID != "" {
			return true
		}
	}
	return false
}
