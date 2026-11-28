package storage

import (
	"altune/go-api/internal/library/internal/domain"
	"altune/go-api/internal/library/internal/ports"
	"context"
	"time"
)

var (
	_ ports.AudioLinkSigner = NoAudioLinks{}
	_ ports.AudioAgeLister  = NoAgeListing{}
)

type NoAudioLinks struct{}

func (NoAudioLinks) SignsLinks() bool { return false }

func (NoAudioLinks) PresignGet(context.Context, string, time.Duration) (string, error) {
	return "", domain.ErrAudioLinksUnsupported
}

type NoAgeListing struct{}

func (NoAgeListing) ListWithAge(context.Context, string) ([]ports.ObjectAge, error) {
	return nil, domain.ErrAgeListingUnsupported
}
