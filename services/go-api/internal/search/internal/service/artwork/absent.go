package artwork

import (
	"altune/go-api/internal/search/internal/core/ports"
	"altune/go-api/internal/search/internal/domain"
	"context"
)

type NoLookup struct{}

func (NoLookup) ResolveTagged(context.Context, domain.ResultKind, string, string, string) (string, domain.ProviderKey, error) {
	return "", "", nil
}

func (NoLookup) ResolveWithIdentityTagged(context.Context, domain.ResultKind, string, string, ports.ArtworkIdentity) (string, domain.ProviderKey, error) {
	return "", "", nil
}

type NoCache struct{}

func (NoCache) Get(context.Context, domain.ResultKind, string, string, string) (string, domain.ProviderKey, bool, error) {
	return "", "", false, nil
}

func (NoCache) Set(context.Context, domain.ResultKind, string, string, string, string, domain.ProviderKey, ports.ArtworkConfidence) error {
	return nil
}

type NoMBIDIndex struct{}

func (NoMBIDIndex) LookupMBID(context.Context, domain.ResultKind, string) (string, bool) {
	return "", false
}

func (NoMBIDIndex) RememberMBID(context.Context, domain.ResultKind, string, string) error {
	return nil
}

type NoIdentities struct{}

func (NoIdentities) LookupByProviderID(context.Context, domain.ResultKind, domain.ProviderKey, string) (string, map[domain.ProviderKey]string, bool) {
	return "", nil, false
}
