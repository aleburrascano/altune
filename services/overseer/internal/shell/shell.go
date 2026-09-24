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

// CollectStatus is the collect loop's liveness as of the moment it is read. The
// loop owns the judgement — it knows its own tick interval and deadlines — and the
// shell only turns Healthy into a status code, so a stalled or dead loop cannot be
// reported green by a handler that never looked at it.
type CollectStatus struct {
	// Healthy reports that a collect cycle completed recently enough to trust. It
	// says nothing about the watched app: a cycle where every bucket's source was
	// down is still a live loop (the outlives-the-app invariant).
	Healthy bool
	// LastCycle is when the most recent cycle finished, zero if none has.
	LastCycle time.Time
	// OK and Failed are that cycle's per-bucket outcome counts.
	OK     int
	Failed int
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
	collectStatus  func() CollectStatus
	series         SeriesReader
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

// WithCollectStatus sets the collect loop's liveness source /health reports. A nil
// func is ignored, keeping the default.
func WithCollectStatus(status func() CollectStatus) Option {
	return func(h *Handler) {
		if status != nil {
			h.collectStatus = status
		}
	}
}

// NewHandler builds the shell handler over the given registry. Without a collect
// status source the shell reports itself healthy: a handler with no loop behind it
// (the HTTP surface alone) can only answer for the socket it is serving on.
func NewHandler(registry Registry, opts ...Option) *Handler {
	h := &Handler{
		registry:       registry,
		streamInterval: defaultStreamInterval,
		collectStatus:  func() CollectStatus { return CollectStatus{Healthy: true} },
		series:         noSeries{},
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// Router returns the mounted routes. Open (no data): /health (the collect loop's
// liveness, green even when the watched app is down), /config.json (public SPA
// config), and the embedded SPA at "/" and its assets. Guarded by the Supabase
// owner-only check: GET /api/buckets, GET /api/buckets/{id}/series and GET
// /api/stream, the only routes that expose watched-app data.
func (h *Handler) Router() http.Handler {
	r := chi.NewRouter()
	r.Get("/health", h.handleHealth)
	r.Get("/config.json", h.handleConfig)
	r.Group(func(r chi.Router) {
		r.Use(OwnerOnly(h.verifier, h.ownerUserID))
		r.Get("/api/buckets", h.handleBuckets)
		r.Get("/api/buckets/{id}/series", h.handleSeries)
		r.Get("/api/stream", h.handleStream)
	})
	// Everything else is the open SPA: index.html and hashed assets carry no
	// watched-app data, and the SPA itself decides login-vs-dashboard from the
	// Supabase session. Registered as GET only so the whole surface stays
	// read-only — there is no mutating (POST/PUT/…) route anywhere.
	r.Get("/*", h.handleStatic)
	return r
}

// healthResponse is the /health body. It carries the collect loop's last outcome so
// a probe that fails says which half is broken — a wedged loop (a stale last_cycle)
// versus a watched app that is down (a fresh cycle with buckets_failed high).
type healthResponse struct {
	Status        string `json:"status"`
	LastCycle     string `json:"last_cycle,omitempty"`
	BucketsOK     int    `json:"buckets_ok"`
	BucketsFailed int    `json:"buckets_failed"`
}

// handleHealth is the open liveness probe the container healthcheck and the off-box
// uptime check read. It is 200 only while the collect loop is live: a listening
// socket over a dead or wedged tickLoop is precisely the failure the backstop exists
// to catch, so a hardcoded ok would make both probes lie.
func (h *Handler) handleHealth(w http.ResponseWriter, _ *http.Request) {
	status := h.collectStatus()
	body := healthResponse{
		Status:        "ok",
		BucketsOK:     status.OK,
		BucketsFailed: status.Failed,
	}
	if !status.LastCycle.IsZero() {
		body.LastCycle = status.LastCycle.UTC().Format(time.RFC3339)
	}
	if !status.Healthy {
		body.Status = "collect_stalled"
		writeJSON(w, http.StatusServiceUnavailable, body)
		return
	}
	writeJSON(w, http.StatusOK, body)
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
