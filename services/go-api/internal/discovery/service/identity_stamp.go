package service

import (
	"context"
	"log/slog"
	"time"

	"altune/go-api/internal/discovery/domain"
)

const identityPersistTimeout = 30 * time.Second

func (s *Service) stampIdentities(ctx context.Context, perProvider [][]domain.SearchResult) {
	if s.identityBridge == nil {
		return
	}
	type learnedBridge struct {
		kind domain.ResultKind
		mbid string
		ids  map[string]string
	}
	var learned []learnedBridge

	for gi := range perProvider {
		for ri := range perProvider[gi] {
			r := &perProvider[gi][ri]
			if r.MBID == "" {
				continue
			}
			ids, ok := s.identityBridge.ExternalIDs(ctx, r.Kind, r.MBID)
			if !ok {
				continue
			}
			r.Xref = ids
			slog.DebugContext(ctx, "merge.identity_bridge_stamped",
				"kind", r.Kind.String(), "mbid", r.MBID, "ids", len(ids))
			if s.identityStore != nil {
				learned = append(learned, learnedBridge{kind: r.Kind, mbid: r.MBID, ids: ids})
			}
		}
	}

	if len(learned) == 0 {
		return
	}
	s.launchBackground(ctx, "identity.persist_bridges", func(bgCtx context.Context) {
		bgCtx, cancel := context.WithTimeout(bgCtx, identityPersistTimeout)
		defer cancel()
		for _, b := range learned {
			ids := b.ids
			if s.identityVerifier != nil {
				var ok bool
				ids, ok = s.identityVerifier.VerifyXref(bgCtx, b.kind, b.mbid, b.ids)
				if !ok {
					continue
				}
			}
			if err := s.identityStore.PersistBridges(bgCtx, b.kind, b.mbid, ids); err != nil {
				slog.WarnContext(bgCtx, "identity.persist_failed",
					"kind", b.kind.String(), "mbid", b.mbid, "error", err)
				s.identityVerifier.Forget(b.mbid)
			}
		}
	})
}
