package handler

import (
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
