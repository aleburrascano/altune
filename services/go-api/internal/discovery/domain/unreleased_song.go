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
	AudioLinks      []AudioLink
}

type UnreleasedSong struct {
	GeniusID     int         `json:"genius_id"`
	Title        string      `json:"title"`
	CoverURL     string      `json:"cover_url,omitempty"`
	GeniusURL    string      `json:"genius_url"`
	ReleaseDate  string      `json:"release_date,omitempty"`
	HasMedia     *bool       `json:"has_media"`
	Quality      string      `json:"quality,omitempty"`
	TrackerKey   string      `json:"tracker_key,omitempty"`
	AudioLinks   []AudioLink `json:"audio_links,omitempty"`
	OffStreaming bool        `json:"off_streaming,omitempty"`
}
