// Package shell is the Overseer platform core: it wires the HTTP surface, serves
// the embedded React SPA, exposes the bucket data as a JSON API + SSE stream, and
// guards every data route behind the Supabase owner-only check. It references the
// Bucket interface and the registry only — never a concrete bucket — which is the
// additive-buckets invariant made real, and it emits no HTML: the API is pure
// JSON, so core no longer imports html/template.
package shell

import (
	"altune/overseer/internal/core"
	"encoding/json"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// defaultStreamInterval is how often the SSE stream re-emits every bucket's
// current snapshot. One user, one browser: a low fixed cadence is ample and keeps
// per-connection work bounded.
const defaultStreamInterval = 2 * time.Second

// Registry is the read side of the bucket registry the shell serves from.
type Registry interface {
	Buckets() []core.Bucket
}

// ClientConfig is the public, non-secret configuration the open /config.json
// endpoint hands the SPA so its supabase-js login can run without the values being
// baked into the build. The anon key is a publishable client key (safe in the
// browser); no watched-app data appears here.
type ClientConfig struct {
	SupabaseURL     string `json:"supabaseUrl"`
	SupabaseAnonKey string `json:"supabaseAnonKey"`
}

// Handler serves the Overseer HTTP surface.
type Handler struct {
	registry       Registry
	verifier       Verifier
	ownerUserID    string
	static         fs.FS
	clientConfig   ClientConfig
	streamInterval time.Duration
}

// Option configures a Handler at construction.
type Option func(*Handler)

// WithVerifier sets the Supabase JWT verifier the owner-only guard uses.
func WithVerifier(v Verifier) Option { return func(h *Handler) { h.verifier = v } }

// WithOwnerUserID sets the single allowlisted owner subject.
func WithOwnerUserID(id string) Option { return func(h *Handler) { h.ownerUserID = id } }

// WithStaticFS sets the embedded SPA file system served open at "/".
func WithStaticFS(f fs.FS) Option { return func(h *Handler) { h.static = f } }

// WithClientConfig sets the public config served at /config.json.
func WithClientConfig(c ClientConfig) Option { return func(h *Handler) { h.clientConfig = c } }

// WithStreamInterval overrides the SSE re-emit cadence. A non-positive value is
// ignored, keeping the default.
func WithStreamInterval(d time.Duration) Option {
	return func(h *Handler) {
		if d > 0 {
			h.streamInterval = d
		}
	}
}

// NewHandler builds the shell handler over the given registry.
func NewHandler(registry Registry, opts ...Option) *Handler {
	h := &Handler{registry: registry, streamInterval: defaultStreamInterval}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// Router returns the mounted routes. Open (no data): /health (uptime backstop even
// when the watched app is down), /config.json (public SPA config), and the
// embedded SPA at "/" and its assets. Guarded by the Supabase owner-only check:
// GET /api/buckets and GET /api/stream, the only routes that expose watched-app
// data.
func (h *Handler) Router() http.Handler {
	r := chi.NewRouter()
	r.Get("/health", handleHealth)
	r.Get("/config.json", h.handleConfig)
	r.Group(func(r chi.Router) {
		r.Use(OwnerOnly(h.verifier, h.ownerUserID))
		r.Get("/api/buckets", h.handleBuckets)
		r.Get("/api/stream", h.handleStream)
	})
	// Everything else is the open SPA: index.html and hashed assets carry no
	// watched-app data, and the SPA itself decides login-vs-dashboard from the
	// Supabase session. Registered as GET only so the whole surface stays
	// read-only — there is no mutating (POST/PUT/…) route anywhere.
	r.Get("/*", h.handleStatic)
	return r
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

// handleConfig serves the public Supabase client config the SPA login needs. It
// is open — it exposes no watched-app data, only publishable client values — and
// never sets a cookie.
func (h *Handler) handleConfig(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, h.clientConfig)
}

// handleStatic serves the embedded SPA. A request for an existing file (a hashed
// asset) is served directly; anything else falls back to index.html so the SPA's
// client-side routing works and a deep link never 404s. It is defensive against a
// nil static FS (unit tests that do not wire the SPA) by returning 404.
func (h *Handler) handleStatic(w http.ResponseWriter, r *http.Request) {
	setSecurityHeaders(w)
	if h.static == nil {
		http.NotFound(w, r)
		return
	}
	clean := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if clean == "" || clean == "." {
		h.serveIndex(w, r)
		return
	}
	f, err := h.static.Open(clean)
	if err != nil {
		h.serveIndex(w, r)
		return
	}
	_ = f.Close()
	http.FileServerFS(h.static).ServeHTTP(w, r)
}

// serveIndex writes the SPA entry document.
func (h *Handler) serveIndex(w http.ResponseWriter, r *http.Request) {
	data, err := fs.ReadFile(h.static, "index.html")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// setSecurityHeaders applies the defense-in-depth headers every Overseer response
// shares: forbid framing (clickjacking) and MIME sniffing.
func setSecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
}

// writeJSON marshals v and writes it with the given status. It never sets a
// cookie.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	body, err := json.Marshal(v)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(status)
	_, _ = w.Write(body)
}
