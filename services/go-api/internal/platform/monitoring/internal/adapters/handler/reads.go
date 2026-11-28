package handler

import (
	"altune/go-api/internal/platform/monitoring/internal/service"
	"altune/go-api/internal/shared/httpserver"
	"net/http"
)

func (h *Handler) serveAcquisition(w http.ResponseWriter, r *http.Request) {
	httpserver.WriteJSON(w, http.StatusOK, readAcquisitionStatus(r.Context(), h.deps.Acquisition))
}

func (h *Handler) serveEval(w http.ResponseWriter, _ *http.Request) {
	if h.deps.Eval == nil {
		httpserver.WriteJSON(w, http.StatusOK, evalStatusDTO{Enabled: false, State: string(service.StateDisabled)})
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, projectEvalStatus(h.deps.Eval.Status()))
}

func projectEvalStatus(st service.MeterStatus) evalStatusDTO {
	out := evalStatusDTO{
		Enabled: st.Enabled, Paused: st.Paused, State: string(st.State),
		Score: st.Score, Baseline: st.Baseline, Errored: st.Errored,
		LastRun: st.LastRun, Error: st.Error,
	}
	for _, q := range st.Queries {
		out.Queries = append(out.Queries, evalQueryDTO{Query: q.Query, Expect: q.Expect, Passed: q.Passed, Position: q.Position})
	}
	return out
}

func (h *Handler) serveMetricsLive(w http.ResponseWriter, _ *http.Request) {
	if h.deps.LiveMetrics == nil {
		httpserver.WriteJSON(w, http.StatusOK, struct{}{})
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, h.deps.LiveMetrics())
}
