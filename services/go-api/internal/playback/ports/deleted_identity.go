package ports

import (
	"altune/go-api/internal/shared"
	"context"
	"errors"
)

// ErrIdentityStoreUnavailable reports that the identity store cannot be read
// from here — absent (a plain Postgres carrying no Supabase auth schema) or not
// granted to this role. Callers idle on it rather than failing: "no identity is
// visible" must never be acted on as "every identity was deleted".
var ErrIdentityStoreUnavailable = errors.New("identity store unavailable")

// DeletedIdentityLister answers which owners of stored queue state no longer
// have an identity. Identities are owned out-of-band by Supabase, which deletes
// an account without telling this service and leaves no cascade behind it, so
// the queue state of a deleted account is findable only by asking.
type DeletedIdentityLister interface {
	// ListOwnersWithoutIdentity returns at most limit (which must be positive)
	// owners whose identity is gone, longest-untouched queue state first.
	// It returns an error satisfying errors.Is(err, ErrIdentityStoreUnavailable)
	// when the identity store cannot be read, and never reports an owner as
	// deleted on the strength of an identity store it could not see.
	ListOwnersWithoutIdentity(ctx context.Context, limit int) ([]shared.UserId, error)
}
