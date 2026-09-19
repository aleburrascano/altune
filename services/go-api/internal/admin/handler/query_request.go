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
	"time"
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

// serveQueryAction runs the shared guard/decode/call/respond flow used by the
// query-driven admin handlers: reject when the dependency is unconfigured (503),
// decode the query body (decodeFailure codes the rejection), invoke action, map
// its failure via inspectorError, and write the action's result as 200 JSON.
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
		slog.String("corr_id", logging.CorrelationIDFromContext(ctx)),
		slog.Time("at", time.Now().UTC()),
	)
}

// operatorActor names the authenticated caller for the audit record. OperatorOnly
// guarantees a user id upstream; "unknown" keeps a mis-wired route visible.
func operatorActor(ctx context.Context) string {
	if id, ok := auth.UserIDFromContext(ctx); ok {
		return id.String()
	}
	return "unknown"
}

// nonNilKinds renders an omitted kinds list as [] rather than null.
func nonNilKinds(kinds []string) []string {
	if kinds == nil {
		return []string{}
	}
	return kinds
}
