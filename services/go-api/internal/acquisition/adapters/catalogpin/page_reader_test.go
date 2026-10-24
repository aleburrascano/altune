package catalogpin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHTTPPageReader_ReadsAPageButRefusesARedirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/hop" {
			http.Redirect(w, r, "http://169.254.169.254/latest", http.StatusFound)
			return
		}
		_, _ = w.Write([]byte("page body"))
	}))
	t.Cleanup(srv.Close)
	reader := NewHTTPPageReader(srv.Client().Transport, time.Second)

	body, err := reader.ReadPage(context.Background(), srv.URL+"/f/x")
	if err != nil || string(body) != "page body" {
		t.Fatalf("ReadPage = %q, %v", body, err)
	}
	if _, err := reader.ReadPage(context.Background(), srv.URL+"/hop"); err == nil || !strings.Contains(err.Error(), "redirected") {
		t.Errorf("ReadPage followed a redirect, err = %v", err)
	}
}
