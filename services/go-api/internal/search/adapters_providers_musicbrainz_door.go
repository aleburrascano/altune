package search

import musicbrainz "altune/go-api/internal/search/internal/adapters/providers/musicbrainz"

type MusicBrainzAdapter = musicbrainz.MusicBrainzAdapter

var NewMusicBrainzAdapter = musicbrainz.NewMusicBrainzAdapter
