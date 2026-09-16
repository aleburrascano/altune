// Package shell is the Overseer platform core: it wires the HTTP surface, serves
// the embedded-UI shell page and drives registered buckets to render their
// panels. It references the Bucket interface and the registry only — never a
// concrete bucket — which is the additive-buckets invariant made real.
package shell

import (
	"altune/overseer/internal/core"
	"embed"
	"html/template"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

//go:embed shell.html login.html
var templatesFS embed.FS

var (
	shellTemplate = template.Must(template.ParseFS(templatesFS, "shell.html"))
	loginTemplate = template.Must(template.ParseFS(templatesFS, "login.html"))
)

// Registry is the read side of the bucket registry the shell renders from.
type Registry interface {
	Buckets() []core.Bucket
}

// Handler serves the Overseer HTTP surface.
type Handler struct {
	registry Registry
	// basePath is the URL prefix Overseer is mounted under, prefixed onto every
	// OUTBOUND path (redirects, form action, cookie path) so they land back inside
	// the mount when a reverse proxy strips the prefix. Inbound routes stay
	// unprefixed. It is "" by default, which reproduces rootless behavior exactly.
	basePath string
}

// Option configures a Handler at construction.
type Option func(*Handler)

// WithBasePath mounts Overseer under the given URL prefix for outbound paths.
// The value is normalized defensively (single leading slash, no trailing slash;
// empty or slash-only collapses to ""), so an odd configured value cannot produce
// a broken or protocol-relative (open-redirect) outbound path. The base is always
// server-configured and never caller-supplied.
func WithBasePath(basePath string) Option {
	return func(h *Handler) { h.basePath = normalizeBasePath(basePath) }
}

// NewHandler builds the shell handler over the given registry. With no options it
// is rootless (basePath ""), byte-identical to the historical behavior.
func NewHandler(registry Registry, opts ...Option) *Handler {
	h := &Handler{registry: registry}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// normalizeBasePath coerces a raw prefix into a safe outbound base: "" stays "";
// a non-empty value gets exactly one leading slash and no trailing slash, so
// base+"/login" never doubles a slash and a "//" form (a protocol-relative URL to
// a browser) can never reach an outbound path. It mirrors config.normalizeBasePath
// so the shell is safe even if constructed directly with a raw value.
func normalizeBasePath(raw string) string {
	p := strings.TrimSpace(raw)
	if p == "" {
		return ""
	}
	p = "/" + strings.TrimLeft(p, "/")
	return strings.TrimRight(p, "/")
}

// Router returns the mounted routes. /health is open so an off-box uptime check
// can confirm Overseer is alive even when the watched app is down; everything
// that exposes Overseer data sits behind the owner-only guard.
func (h *Handler) Router(ownerToken string) http.Handler {
	r := chi.NewRouter()
	r.Get("/health", handleHealth)
	// The login form and its POST sit outside the owner-only guard — they are how
	// a browser acquires the cookie — and expose no Overseer data, only the form.
	r.Get("/login", h.handleLoginForm)
	r.Post("/login", h.handleLoginSubmit(ownerToken))
	r.Group(func(r chi.Router) {
		r.Use(OwnerOnly(ownerToken, h.basePath))
		r.Get("/", h.handleShell)
	})
	return r
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

type shellView struct {
	PanelCount int
	Panels     []core.Panel
}

func (h *Handler) handleShell(w http.ResponseWriter, r *http.Request) {
	panels := h.renderPanels()
	view := shellView{PanelCount: len(panels), Panels: panels}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	setSecurityHeaders(w)
	if err := shellTemplate.Execute(w, view); err != nil {
		slog.ErrorContext(r.Context(), "overseer.shell.render", "error", err)
	}
}

// setSecurityHeaders applies the defense-in-depth headers shared by every HTML
// page Overseer serves: forbid framing (clickjacking) and MIME sniffing, and add
// a CSP frame-ancestors layer. Both the owner-only shell and the login form use
// it, so the token-entry page is as hardened as the data page.
func setSecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
}

// renderPanels asks every bucket for its panel. A single bucket must not be able
// to take down the shell, so a panic in one bucket's Render is contained and
// replaced with a degraded panel.
func (h *Handler) renderPanels() []core.Panel {
	buckets := h.registry.Buckets()
	panels := make([]core.Panel, 0, len(buckets))
	for _, b := range buckets {
		panels = append(panels, safeRender(b))
	}
	return panels
}

func safeRender(b core.Bucket) (panel core.Panel) {
	meta := b.Meta()
	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("overseer.shell.panel_panic", "bucket", meta.ID, "recover", rec)
			panel = core.Panel{Title: meta.Title, Body: template.HTML("<p class=\"empty\">panel unavailable</p>")} //nolint:gosec // static literal, no user input
		}
	}()
	return b.Render()
}
