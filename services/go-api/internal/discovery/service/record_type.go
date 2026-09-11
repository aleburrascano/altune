package service

type RecordType string

const (
	RecordTypeUnknown     RecordType = ""
	RecordTypeAlbum       RecordType = "album"
	RecordTypeSingle      RecordType = "single"
	RecordTypeEP          RecordType = "ep"
	RecordTypeCompilation RecordType = "compilation"
)

func ParseRecordType(s string) RecordType {
	switch RecordType(s) {
	case RecordTypeAlbum:
		return RecordTypeAlbum
	case RecordTypeSingle:
		return RecordTypeSingle
	case RecordTypeEP:
		return RecordTypeEP
	case RecordTypeCompilation:
		return RecordTypeCompilation
	default:
		return RecordTypeUnknown
	}
}

func (rt RecordType) Rank() int {
	switch rt {
	case RecordTypeSingle, RecordTypeEP, RecordTypeCompilation:
		return 2
	case RecordTypeAlbum:
		return 1
	default:
		return 0
	}
}
