package grade

type SourceKind string

const (
	ArtTrack      SourceKind = "art_track"
	OfficialVideo SourceKind = "official_video"
	LabelUpload   SourceKind = "label_upload"
	FanUpload     SourceKind = "fan_upload"
	UserUpload    SourceKind = "user_upload"
)

func (k SourceKind) IsOfficial() bool {
	return k == ArtTrack || k == OfficialVideo || k == LabelUpload
}
