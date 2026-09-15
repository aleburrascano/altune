package shell_test

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/shell"
	"context"
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type panelBucket struct {
	id    string
	title string
	body  string
}

func (p panelBucket) Meta() core.Meta                                { return core.Meta{ID: p.id, Title: p.title} }
func (p panelBucket) Collect(context.Context) ([]core.Signal, error) { return nil, nil }
func (p panelBucket) Store([]core.Signal)                            {}
func (p panelBucket) Render() core.Panel {
	return core.Panel{Title: p.title, Body: template.HTML(p.body)} //nolint:gosec // test fixture
}

type fixedRegistry struct{ buckets []core.Bucket }

func (f fixedRegistry) Buckets() []core.Bucket { return f.buckets }

// The shell renders each registered bucket's panel into the served page.
func TestShellRendersBucketPanels(t *testing.T) {
	reg := fixedRegistry{buckets: []core.Bucket{
		panelBucket{id: "hb", title: "Heartbeat", body: "<p>alive</p>"},
	}}
	srv := shell.NewHandler(reg).Router(testToken)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	body := rec.Body.String()
	for _, want := range []string{"Heartbeat", "<p>alive</p>", "1 bucket(s) reporting"} {
		if !strings.Contains(body, want) {
			t.Errorf("shell body missing %q\n---\n%s", want, body)
		}
	}
}

// A panicking bucket must not take down the shell: its panel degrades, the rest
// render, and the response is still 200.
func TestShellSurvivesPanickingBucket(t *testing.T) {
	reg := fixedRegistry{buckets: []core.Bucket{
		panicBucket{},
		panelBucket{id: "ok", title: "Healthy", body: "<p>ok</p>"},
	}}
	srv := shell.NewHandler(reg).Router(testToken)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 despite panicking bucket", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Healthy") {
		t.Error("healthy panel missing after sibling panicked")
	}
}

type panicBucket struct{}

func (panicBucket) Meta() core.Meta                                { return core.Meta{ID: "boom", Title: "Boom"} }
func (panicBucket) Collect(context.Context) ([]core.Signal, error) { return nil, nil }
func (panicBucket) Store([]core.Signal)                            {}
func (panicBucket) Render() core.Panel                             { panic("bucket render blew up") }
