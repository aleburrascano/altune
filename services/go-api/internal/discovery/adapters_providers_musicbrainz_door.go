package discovery

import musicbrainz "altune/go-api/internal/discovery/internal/adapters/providers/musicbrainz"

type MusicBrainzAdapter = musicbrainz.MusicBrainzAdapter

var NewMusicBrainzAdapter = musicbrainz.NewMusicBrainzAdapter
