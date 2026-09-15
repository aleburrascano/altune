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

	"github.com/go-chi/chi/v5"
)

//go:embed shell.html
var templatesFS embed.FS

var shellTemplate = template.Must(template.ParseFS(templatesFS, "shell.html"))

// Registry is the read side of the bucket registry the shell renders from.
type Registry interface {
	Buckets() []core.Bucket
}

// Handler serves the Overseer HTTP surface.
type Handler struct {
	registry Registry
}

// NewHandler builds the shell handler over the given registry.
func NewHandler(registry Registry) *Handler {
	return &Handler{registry: registry}
}

// Router returns the mounted routes. /health is open so an off-box uptime check
// can confirm Overseer is alive even when the watched app is down; everything
// that exposes Overseer data sits behind the owner-only guard.
func (h *Handler) Router(ownerToken string) http.Handler {
	r := chi.NewRouter()
	r.Get("/health", handleHealth)
	r.Group(func(r chi.Router) {
		r.Use(OwnerOnly(ownerToken))
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
	if err := shellTemplate.Execute(w, view); err != nil {
		slog.ErrorContext(r.Context(), "overseer.shell.render", "error", err)
	}
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
