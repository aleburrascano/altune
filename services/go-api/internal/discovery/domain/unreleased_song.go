package domain

type AudioLink struct {
	Provider string `json:"provider"`
	URL      string `json:"url"`
}

type GeniusSongDetail struct {
	HasMedia        bool
	OnStreamingSite bool
	AlbumTitle      string
	AudioLinks      []AudioLink
}

type UnreleasedSong struct {
	GeniusID    int         `json:"genius_id"`
	Title       string      `json:"title"`
	CoverURL    string      `json:"cover_url,omitempty"`
	GeniusURL   string      `json:"genius_url"`
	ReleaseDate string      `json:"release_date,omitempty"`
	HasMedia    *bool       `json:"has_media"`
	AudioLinks  []AudioLink `json:"audio_links,omitempty"`
}
