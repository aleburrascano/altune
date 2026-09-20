package service

import "altune/go-api/internal/discovery/domain"

func NormalizeRecordType(m MergedRelease) domain.RecordType {
	if m.Result.TrackCount == 1 {
		return domain.RecordTypeSingle
	}
	switch domain.ParseRecordType(string(m.Result.RecordType)) {
	case domain.RecordTypeSingle:
		return domain.RecordTypeSingle
	case domain.RecordTypeEP:
		return domain.RecordTypeEP
	default:
		return domain.RecordTypeAlbum
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
		case domain.RecordTypeSingle:
			b.Singles = append(b.Singles, m)
		case domain.RecordTypeEP:
			b.EPs = append(b.EPs, m)
		default:
			b.Albums = append(b.Albums, m)
		}
	}
	return b
}
