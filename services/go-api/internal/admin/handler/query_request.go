package handler

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/shared/httputil"
	"altune/go-api/internal/shared/logging"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type queryRequest struct {
	Query string   `json:"query"`
	Kinds []string `json:"kinds"`
}

func decodeQuery(w http.ResponseWriter, r *http.Request) (queryRequest, bool) {
	var body queryRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httputil.HandleServiceError(w, r, decodeFailure(err))
		return queryRequest{}, false
	}
	if body.Query == "" {
		httputil.HandleServiceError(w, r, errQueryRequired)
		return queryRequest{}, false
	}
	return body, true
}

// decodeFailure tells apart the three ways a query body fails to arrive, each
// of which asks a different fix of the caller (#2006): no body at all is a
// missing query, a body past the server's ceiling is 413, and anything else is
// malformed JSON.
func decodeFailure(err error) *codedError {
	var tooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &tooLarge):
		return errBodyTooLarge
	case errors.Is(err, io.EOF):
		return errQueryRequired
	default:
		return errInvalidJSON
	}
}

// serveQueryAction runs the shared guard/admit/decode/call/respond flow used by
// the query-driven admin handlers: reject when the dependency is unconfigured
// (503), take an inspector replay slot (429 when the caller is over either
// limit), decode the query body (decodeFailure codes the rejection), invoke
// action, map its failure via inspectorError, and write the result as 200 JSON.
// Per-handler differences (whether kinds is forwarded, and the response shape)
// live in the action closure so each endpoint's output is byte-for-byte
// unchanged.
func (h *AdminHandler) serveQueryAction(
	w http.ResponseWriter,
	r *http.Request,
	configured bool,
	unavailable error,
	failCode string,
	action func(ctx context.Context, body queryRequest) (any, error),
) {
	if !configured {
		httputil.HandleServiceError(w, r, unavailable)
		return
	}
	release, refused := inspectorReplays.admit(r.Context())
	if refused != nil {
		logShedReplay(r.Context(), refused)
		httputil.HandleServiceError(w, r, refused)
		return
	}
	defer release()
	body, ok := decodeQuery(w, r)
	if !ok {
		return
	}
	result, err := action(r.Context(), body)
	if err != nil {
		httputil.HandleServiceError(w, r, inspectorError(r.Context(), failCode, err))
		return
	}
	httputil.WriteJSON(w, http.StatusOK, result)
}

// auditOperatorAction emits the structured audit record for an operator action
// that re-issues real requests to third-party providers (#999), so who ran it,
// what was rerun, and when survives after the response is sent. It is called
// after the body decodes and before the action runs, so a rerun that fails
// upstream — having already generated provider traffic — is still recorded.
// The query is logged as a length + process-scoped fingerprint, never raw:
// rerun queries are routinely replayed user search text, which must not reach
// stdout logs (#1097).
func auditOperatorAction(ctx context.Context, action string, body queryRequest) {
	slog.InfoContext(ctx, "admin.operator_action",
		slog.String("action", action),
		slog.String("actor", operatorActor(ctx)),
		logging.SearchTextAttr(body.Query),
		slog.Any("kinds", nonNilKinds(body.Kinds)),
		logging.CorrelationAttr(ctx),
		slog.Time("at", time.Now().UTC()),
	)
}

// nonNilKinds renders an omitted kinds list as [] rather than null.
func nonNilKinds(kinds []string) []string {
	if kinds == nil {
		return []string{}
	}
	return kinds
}

// The three inspector routes replay the real discovery pipeline, so one call is
// a fresh fan-out to Apple, SoundCloud, MusicBrainz and the rest. A script or a
// leaked operator token driving them in a loop gets this server's IP throttled
// by those providers and degrades live user search, so the routes share one
// admission budget (#1996).
const (
	// maxConcurrentReplays is how many replays may run at once across all three
	// routes. Two lets an operator compare a rerun against a detail rerun
	// without letting a loop stack fan-outs.
	maxConcurrentReplays = 2

	// replayInterval and replayBurst size one operator's token bucket: a
	// handful of replays back to back while reading the console, then one per
	// interval, which stays far under what hand-driven debugging needs.
	replayInterval = 2 * time.Second
	replayBurst    = 5

	// busyRetryAfter is the hint a shed caller gets. A running replay holds its
	// slot for at most the inspector budget and usually for the few seconds a
	// fan-out takes.
	busyRetryAfter = 5 * time.Second
)

// inspectorGate admits inspector replays: an in-flight cap shared by the three
// routes, and one token bucket per operator.
type inspectorGate struct {
	inFlight chan struct{}

	mu      sync.Mutex
	buckets map[string]*rate.Limiter
}

// inspectorReplays is one gate for the whole process because what it protects
// is a per-process resource: this server's standing with the third-party
// providers, which a per-handler gate would let every handler spend in full.
var inspectorReplays = newInspectorGate()

func newInspectorGate() *inspectorGate {
	return &inspectorGate{
		inFlight: make(chan struct{}, maxConcurrentReplays),
		buckets:  make(map[string]*rate.Limiter),
	}
}

// admit takes one replay slot, returning the release to call once the replay is
// done, or the 429 to answer with. A refusal holds no slot; it still spends the
// caller's token, so a client hammering a busy gate slows itself down.
func (g *inspectorGate) admit(ctx context.Context) (func(), *codedError) {
	if wait, admitted := g.spendToken(ctx); !admitted {
		return nil, replayThrottled(wait)
	}
	select {
	case g.inFlight <- struct{}{}:
		return func() { <-g.inFlight }, nil
	default:
		return nil, errReplaySlotsBusy
	}
}

// spendToken takes one of the calling operator's tokens, reporting how long
// until the next one when the bucket is empty. A request carrying no principal
// spends nothing: every inspector route sits behind the admin gate, so only a
// test or a misrouted mount reaches here unauthenticated, and the in-flight cap
// bounds those — the same rule the discovery and playback throttles follow.
func (g *inspectorGate) spendToken(ctx context.Context) (time.Duration, bool) {
	principal, authenticated := auth.UserIDFromContext(ctx)
	if !authenticated {
		return 0, true
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	bucket := g.bucket(principal.String())
	if bucket.Allow() {
		return 0, true
	}
	reservation := bucket.Reserve()
	defer reservation.Cancel()
	return reservation.Delay(), false
}

// bucket is principal's token bucket, created on first sight. The map is bounded
// by the principals the admin gate admits — the configured operator ids — so it
// cannot grow with request volume.
func (g *inspectorGate) bucket(principal string) *rate.Limiter {
	if existing, ok := g.buckets[principal]; ok {
		return existing
	}
	created := rate.NewLimiter(rate.Every(replayInterval), replayBurst)
	g.buckets[principal] = created
	return created
}

// logShedReplay records a refused replay. The 429 reaches the caller and
// nowhere else, so this line is the only signal that something is looping on
// the inspector routes.
func logShedReplay(ctx context.Context, refused *codedError) {
	slog.WarnContext(ctx, "admin.inspector_shed",
		slog.String("code", refused.ErrorCode()),
		slog.String("actor", operatorActor(ctx)),
		logging.CorrelationAttr(ctx),
	)
}
