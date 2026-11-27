package audiofiles

import (
	"altune/go-api/internal/library/internal/core/ports"
	"context"
	"errors"
	"time"
)

var (
	ErrAudioLinksUnsupported = errors.New("the configured audio store cannot sign links")
	ErrAgeListingUnsupported = errors.New("the configured audio store cannot list object age")
)

var (
	_ ports.AudioLinkSigner = NoAudioLinks{}
	_ ports.AudioAgeLister  = NoAgeListing{}
)

type NoAudioLinks struct{}

func (NoAudioLinks) SignsLinks() bool { return false }

func (NoAudioLinks) PresignGet(context.Context, string, time.Duration) (string, error) {
	return "", ErrAudioLinksUnsupported
}

type NoAgeListing struct{}

func (NoAgeListing) ListWithAge(context.Context, string) ([]ports.ObjectAge, error) {
	return nil, ErrAgeListingUnsupported
}
