package domain

type AudioProvider string

const (
	AudioProviderSoundCloud AudioProvider = "soundcloud"
	AudioProviderYouTube    AudioProvider = "youtube"
)

type GeniusSongDetail struct {
	HasMedia        bool
	OnStreamingSite bool
	AlbumTitle      string
	AlbumCoverURL   string
	AudioLinks      []AudioLink
}

type UnreleasedSong struct {
	GeniusID     int
	Title        string
	CoverURL     string
	GeniusURL    string
	ReleaseDate  string
	HasMedia     *bool
	Quality      string
	TrackerKey   string
	AudioLinks   []AudioLink
	OffStreaming bool
	OpenURL      string
	OpenHost     string

	Versions []UnreleasedVersion
}

type UnreleasedVersion struct {
	Title      string
	Label      string
	Quality    string
	AudioLinks []AudioLink
	OpenURL    string
	OpenHost   string
}
