package domain

import (
	"altune/go-api/internal/shared/songfacts"
	"strings"
)

type TrackPosition struct {
	Disc  int
	Track int
}

type ContributorRole string

const (
	ContributorMain     ContributorRole = "main"
	ContributorFeatured ContributorRole = "featured"
)

type Contributor struct {
	ProviderID string
	Name       string
	Role       ContributorRole
}

type RecordTypeVocabulary map[string]RecordType

func (v RecordTypeVocabulary) Parse(raw string) RecordType {
	return v[strings.ToLower(strings.TrimSpace(raw))]
}

type ArtworkPath string

const (
	ArtworkPathNone            ArtworkPath = "none"
	ArtworkPathProvider        ArtworkPath = "provider"
	ArtworkPathCache           ArtworkPath = "cache"
	ArtworkPathDegraded        ArtworkPath = "degraded"
	ArtworkPathPending         ArtworkPath = "pending"
	ArtworkPathName            ArtworkPath = "name"
	ArtworkPathIdentity        ArtworkPath = "identity"
	ArtworkPathDurableIdentity ArtworkPath = "durable-identity"
)

func BoolPtr(v bool) *bool { return &v }

func ValidISRC(raw string) string {
	canonical := songfacts.CanonicalISRC(raw)
	if songfacts.IsCanonicalISRC(canonical) {
		return canonical
	}
	return ""
}
