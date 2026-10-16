package domain

type GeniusSongDetail struct {
	HasMedia        bool
	OnStreamingSite bool
	AlbumTitle      string
}

type UnreleasedSong struct {
	GeniusID    int    `json:"genius_id"`
	Title       string `json:"title"`
	CoverURL    string `json:"cover_url,omitempty"`
	GeniusURL   string `json:"genius_url"`
	ReleaseDate string `json:"release_date,omitempty"`
	HasMedia    *bool  `json:"has_media"`
}
