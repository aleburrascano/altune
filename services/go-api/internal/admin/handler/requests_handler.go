package handler

import (
	"altune/go-api/internal/admin/requeststore"
	"altune/go-api/internal/shared/httputil"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

// userDigestBytes is how much of a user id's SHA-256 the drill-down carries:
// enough to tell two callers apart across the records of one retention window,
// and not the Supabase id itself.
const userDigestBytes = 4

// requestSummaryDTO is the /admin/requests list projection: an allowlist, not
// the stored record. The record accumulates the end user's id and every raw
// provider body, none of which a list of traces needs, so a field added to the
// record later cannot reach the list without being named here.
type requestSummaryDTO struct {
	CorrID        string            `json:"corr_id"`
	StartedAt     time.Time         `json:"started_at"`
	Query         string            `json:"query,omitempty"`
	Kinds         []string          `json:"kinds,omitempty"`
	ProviderCount int               `json:"provider_count"`
	FinalCount    int               `json:"final_count"`
	ExchangeCount int               `json:"exchange_count"`
	Detail        *detailSummaryDTO `json:"detail,omitempty"`
}

type detailSummaryDTO struct {
	Kind      string `json:"kind"`
	Provider  string `json:"provider"`
	Artist    string `json:"artist,omitempty"`
	Status    string `json:"status"`
	ItemCount int    `json:"item_count"`
}

// requestDetailDTO is the drill-down projection. It carries the bodies an
// operator debugs a single trace with, but identifies the caller by digest
// only: the console never holds a Supabase id.
type requestDetailDTO struct {
	CorrID    string                       `json:"corr_id"`
	StartedAt time.Time                    `json:"started_at"`
	Exchanges []requeststore.Exchange      `json:"exchanges"`
	Query     string                       `json:"query,omitempty"`
	Kinds     []string                     `json:"kinds,omitempty"`
	User      string                       `json:"user,omitempty"`
	Providers []requeststore.ProviderTrace `json:"providers,omitempty"`
	Final     []requeststore.ResultRow     `json:"final,omitempty"`
	Detail    *requeststore.DetailTrace    `json:"detail,omitempty"`
}

func (h *AdminHandler) serveRequests(w http.ResponseWriter, _ *http.Request) {
	if h.requests == nil {
		httputil.WriteJSON(w, http.StatusOK, []requestSummaryDTO{})
		return
	}
	httputil.WriteJSON(w, http.StatusOK, requestSummaries(h.requests.Snapshot()))
}

func (h *AdminHandler) serveRequestDetail(w http.ResponseWriter, r *http.Request) {
	if h.requests == nil {
		httputil.HandleServiceError(w, r, errRequestNotFound)
		return
	}
	rec, ok := h.requests.Get(chi.URLParam(r, "corrID"))
	if !ok {
		httputil.HandleServiceError(w, r, errRequestNotFound)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, requestDetail(rec))
}

func requestSummaries(records []requeststore.RequestRecord) []requestSummaryDTO {
	out := make([]requestSummaryDTO, 0, len(records))
	for _, rec := range records {
		out = append(out, requestSummaryDTO{
			CorrID:        rec.CorrID,
			StartedAt:     rec.StartedAt,
			Query:         rec.Query,
			Kinds:         rec.Kinds,
			ProviderCount: len(rec.Providers),
			FinalCount:    len(rec.Final),
			ExchangeCount: len(rec.Exchanges),
			Detail:        detailSummary(rec.Detail),
		})
	}
	return out
}

func detailSummary(trace *requeststore.DetailTrace) *detailSummaryDTO {
	if trace == nil {
		return nil
	}
	return &detailSummaryDTO{
		Kind:      trace.Kind,
		Provider:  trace.Provider,
		Artist:    trace.Artist,
		Status:    trace.Status,
		ItemCount: len(trace.Items),
	}
}

func requestDetail(rec requeststore.RequestRecord) requestDetailDTO {
	return requestDetailDTO{
		CorrID:    rec.CorrID,
		StartedAt: rec.StartedAt,
		Exchanges: rec.Exchanges,
		Query:     rec.Query,
		Kinds:     rec.Kinds,
		User:      userDigest(rec.User),
		Providers: rec.Providers,
		Final:     rec.Final,
		Detail:    rec.Detail,
	}
}

func userDigest(user string) string {
	if user == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(user))
	return hex.EncodeToString(sum[:userDigestBytes])
}
