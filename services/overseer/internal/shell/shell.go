package shell

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/goapi"
	"encoding/json"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

const defaultStreamInterval = 2 * time.Second

type Registry interface {
	Buckets() []core.Bucket
	Get(id string) (core.Bucket, bool)
}

type CollectStatus struct {
	Healthy   bool
	LastCycle time.Time
	OK        int
	Failed    int
}

type ClientConfig struct {
	SupabaseURL     string `json:"supabaseUrl"`
	SupabaseAnonKey string `json:"supabaseAnonKey"`
}

type Handler struct {
	registry         Registry
	verifier         Verifier
	ownerUserID      string
	static           fs.FS
	clientConfig     ClientConfig
	streamInterval   time.Duration
	collectStatus    func() CollectStatus
	credentialHealth func() goapi.CredentialHealth
	series           SeriesReader
	sparkCache       *sparkCache
}

type Option func(*Handler)

func WithVerifier(v Verifier) Option { return func(h *Handler) { h.verifier = v } }

func WithOwnerUserID(id string) Option { return func(h *Handler) { h.ownerUserID = id } }

func WithStaticFS(f fs.FS) Option { return func(h *Handler) { h.static = f } }

func WithClientConfig(c ClientConfig) Option { return func(h *Handler) { h.clientConfig = c } }

func WithStreamInterval(d time.Duration) Option {
	return func(h *Handler) {
		if d > 0 {
			h.streamInterval = d
		}
	}
}

func WithCollectStatus(status func() CollectStatus) Option {
	return func(h *Handler) {
		if status != nil {
			h.collectStatus = status
		}
	}
}

func NewHandler(registry Registry, opts ...Option) *Handler {
	h := &Handler{
		registry:       registry,
		streamInterval: defaultStreamInterval,
		collectStatus:  func() CollectStatus { return CollectStatus{Healthy: true} },
		series:         noSeries{},
		sparkCache:     newSparkCache(),
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

func (h *Handler) Router() http.Handler {
	r := chi.NewRouter()
	r.Get("/health", h.handleHealth)
	r.Get("/config.json", h.handleConfig)
	r.Group(func(r chi.Router) {
		r.Use(OwnerOnly(h.verifier, h.ownerUserID))
		r.Get("/api/buckets", h.handleBuckets)
		r.Get("/api/buckets/{id}/series", h.handleSeries)
		r.Get("/api/stream", h.handleStream)
		r.Get("/api/health", h.handleOwnerHealth)
	})
	r.Get("/*", h.handleStatic)
	return r
}

type healthResponse struct {
	Status        string `json:"status"`
	LastCycle     string `json:"last_cycle,omitempty"`
	BucketsOK     int    `json:"buckets_ok"`
	BucketsFailed int    `json:"buckets_failed"`
}

func (h *Handler) handleHealth(w http.ResponseWriter, _ *http.Request) {
	status := h.collectStatus()
	body := healthResponse{
		Status:        "ok",
		LastCycle:     rfc3339OrEmpty(status.LastCycle),
		BucketsOK:     status.OK,
		BucketsFailed: status.Failed,
	}
	if !status.Healthy {
		body.Status = "collect_stalled"
		writeJSON(w, http.StatusServiceUnavailable, body)
		return
	}
	writeJSON(w, http.StatusOK, body)
}

func (h *Handler) handleConfig(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, h.clientConfig)
}

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

func setSecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
}

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
