package unreleased

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"log/slog"
)

type coverage struct {
	Total    int
	Versions int
	Playable int
	LinkOut  int
	NoAudio  int
	NoCover  int
	Hosts    map[string]int
}

func countCoverage(songs []domain.UnreleasedSong) coverage {
	c := coverage{Total: len(songs), Hosts: map[string]int{}}
	for _, song := range songs {
		c.Versions += len(song.Versions)
		switch {
		case len(song.AudioLinks) > 0:
			c.Playable++
		case song.OpenURL != "":
			c.LinkOut++
			c.Hosts[song.OpenHost]++
		default:
			c.NoAudio++
		}
		if song.CoverURL == "" {
			c.NoCover++
		}
	}
	return c
}

func logCoverage(ctx context.Context, provider domain.ProviderName, externalID, artistName string, songs []domain.UnreleasedSong, complete bool) {
	c := countCoverage(songs)
	slog.InfoContext(ctx, "unreleased.coverage",
		"provider", provider,
		"external_id", externalID,
		"artist", artistName,
		"total", c.Total,
		"versions", c.Versions,
		"playable", c.Playable,
		"link_out", c.LinkOut,
		"no_audio", c.NoAudio,
		"no_cover", c.NoCover,
		"complete", complete,
		"hosts", c.Hosts,
	)
}
