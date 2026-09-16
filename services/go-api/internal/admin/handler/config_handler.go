package handler

import (
	"altune/go-api/internal/shared/httputil"
	"net/http"
)

type ConsoleConfig struct {
	SupabaseURL string `json:"supabase_url"`
	AnonKey     string `json:"anon_key"`
}

func (h *AdminHandler) ServeConfig(w http.ResponseWriter, _ *http.Request) {
	httputil.WriteJSON(w, http.StatusOK, ConsoleConfig{
		SupabaseURL: h.supabaseURL,
		AnonKey:     h.supabaseAnonKey,
	})
}
