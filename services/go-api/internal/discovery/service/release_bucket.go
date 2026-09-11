package service

func NormalizeRecordType(m MergedRelease) string {
	if m.Result.TrackCount == 1 {
		return string(RecordTypeSingle)
	}
	switch ParseRecordType(stringExtra(m.Result.Extras, "record_type")) {
	case RecordTypeSingle:
		return string(RecordTypeSingle)
	case RecordTypeEP:
		return string(RecordTypeEP)
	default:
		return string(RecordTypeAlbum)
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
