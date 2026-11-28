package domain

type FetchedAudio struct {
	Path   string
	Format SourceFormat
}

type SourceFormat struct {
	ID    string
	Codec string
	Kbps  int
}

const MuxedFallbackFormatID = "18"

func (f SourceFormat) IsLowBitrate(floorKbps int) bool {
	if floorKbps <= 0 {
		return false
	}
	return f.ID == MuxedFallbackFormatID || (f.Kbps > 0 && f.Kbps < floorKbps)
}
