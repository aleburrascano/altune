package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"context"
	"log/slog"
	"time"
)

const identityPersistTimeout = 30 * time.Second

// IdentityStamper is the identity collaborator: it stamps bridged external IDs
// (xref) onto MBID-bearing provider results before merge, and persists newly
// learned bridges — verified first when a verifier is wired — to the durable
// identity store off the request path. It replaces the identityBridge,
// identityStore and identityVerifier fields that used to sit on the Service god
// object, so identity handling can change (or be tested)
// without constructing the rest of the search pipeline. A nil bridge disables
// stamping.
type IdentityStamper struct {
	bridge   ports.IdentityBridge
	store    ports.IdentityStore
	verifier *IdentityVerifier
	bg       *backgroundRunner
}

func newIdentityStamper(
	bridge ports.IdentityBridge,
	store ports.IdentityStore,
	verifier *IdentityVerifier,
	bg *backgroundRunner,
) *IdentityStamper {
	return &IdentityStamper{bridge: bridge, store: store, verifier: verifier, bg: bg}
}

type learnedBridge struct {
	kind domain.ResultKind
	mbid string
	ids  map[string]string
}

func (s *IdentityStamper) stamp(ctx context.Context, perProvider [][]domain.SearchResult) {
	if s.bridge == nil {
		return
	}
	learned := s.stampXrefs(ctx, perProvider)
	if len(learned) == 0 {
		return
	}
	s.bg.launch(ctx, "identity.persist_bridges", func(bgCtx context.Context) {
		s.persist(bgCtx, learned)
	})
}

// stampXrefs stamps bridged external IDs onto every MBID-bearing result in
// place and returns the bridges worth persisting (none without a store).
func (s *IdentityStamper) stampXrefs(ctx context.Context, perProvider [][]domain.SearchResult) []learnedBridge {
	var learned []learnedBridge

	for gi := range perProvider {
		for ri := range perProvider[gi] {
			r := &perProvider[gi][ri]
			if r.MBID == "" {
				continue
			}
			ids, ok := s.bridge.ExternalIDs(ctx, r.Kind, r.MBID)
			if !ok {
				continue
			}
			r.Xref = ids
			slog.DebugContext(ctx, "merge.identity_bridge_stamped",
				"kind", r.Kind.String(), "mbid", r.MBID, "ids", len(ids))
			if s.store != nil {
				learned = append(learned, learnedBridge{kind: r.Kind, mbid: r.MBID, ids: ids})
			}
		}
	}

	return learned
}

// persist verifies (when a verifier is wired) and durably stores each learned
// bridge under identityPersistTimeout. A failed write forgets the verify memo
// so the next sighting re-verifies instead of silently skipping.
func (s *IdentityStamper) persist(bgCtx context.Context, learned []learnedBridge) {
	bgCtx, cancel := context.WithTimeout(bgCtx, identityPersistTimeout)
	defer cancel()
	for _, b := range learned {
		ids := b.ids
		if s.verifier != nil {
			var ok bool
			ids, ok = s.verifier.VerifyXref(bgCtx, b.kind, b.mbid, b.ids)
			if !ok {
				continue
			}
		}
		if err := s.store.PersistBridges(bgCtx, b.kind, b.mbid, ids); err != nil {
			slog.WarnContext(bgCtx, "identity.persist_failed",
				"kind", b.kind.String(), "mbid", b.mbid, "error", err)
			s.verifier.Forget(b.mbid)
		}
	}
}
