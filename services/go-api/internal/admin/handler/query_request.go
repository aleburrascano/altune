package handler

import (
	"context"
	"encoding/json"
	"net/http"

	"altune/go-api/internal/shared/httputil"
)

type queryRequest struct {
	Query string   `json:"query"`
	Kinds []string `json:"kinds"`
}

func decodeQuery(w http.ResponseWriter, r *http.Request) (queryRequest, bool) {
	var body queryRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Query == "" {
		httputil.WriteError(w, http.StatusBadRequest, "query is required")
		return queryRequest{}, false
	}
	return body, true
}

// serveQueryAction runs the shared guard/decode/call/respond flow used by the
// query-driven admin handlers: reject when the dependency is unconfigured (503),
// decode the query body (400 on failure), invoke action, map any failure to a
// 502 with failCode, and write the action's result as 200 JSON. Per-handler
// differences (whether kinds is forwarded, and the response shape) live in the
// action closure so each endpoint's output is byte-for-byte unchanged.
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
		httputil.HandleServiceError(w, r, upstreamError(failCode, err))
		return
	}
	httputil.WriteJSON(w, http.StatusOK, result)
}
