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
