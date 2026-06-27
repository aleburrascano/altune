package handler

import (
	"net/http"

	"altune/go-api/internal/shared/httputil"
)

// ConsoleConfig is the public bootstrap the console page needs to sign the
// operator in against Supabase directly. Both values are public client config
// (the project URL and the publishable/anon key), safe to expose.
type ConsoleConfig struct {
	SupabaseURL string `json:"supabase_url"`
	AnonKey     string `json:"anon_key"`
}

// ServeConfig returns the public console bootstrap config (unauthenticated — it
// carries no secrets, only the same public client values the mobile app ships).
func (h *AdminHandler) ServeConfig(w http.ResponseWriter, _ *http.Request) {
	httputil.WriteJSON(w, http.StatusOK, ConsoleConfig{
		SupabaseURL: h.supabaseURL,
		AnonKey:     h.supabaseAnonKey,
	})
}
