package service

func NormalizeRecordType(m MergedRelease) string {
	if m.Result.TrackCount == 1 {
		return "single"
	}
	switch stringExtra(m.Result.Extras, "record_type") {
	case "single":
		return "single"
	case "ep":
		return "ep"
	default:
		return "album"
	}
}

type DiscographyBuckets struct {
	Albums  []MergedRelease
	Singles []MergedRelease
	EPs     []MergedRelease
}

func BucketDiscography(releases []MergedRelease) DiscographyBuckets {
	var b DiscographyBuckets
	for _, m := range releases {
		switch NormalizeRecordType(m) {
		case "single":
			b.Singles = append(b.Singles, m)
		case "ep":
			b.EPs = append(b.EPs, m)
		default:
			b.Albums = append(b.Albums, m)
		}
	}
	return b
}
