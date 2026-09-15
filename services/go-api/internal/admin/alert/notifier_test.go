package alert

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

type failingTransport struct{ err error }

func (f failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, f.err
}

func TestNtfyNotifier_MasksURLOnTransportFailure(t *testing.T) {
	const topic = "super-secret-topic"
	const rawURL = "https://ntfy.example.com/" + topic
	n := &NtfyNotifier{
		url:    rawURL,
		client: &http.Client{Transport: failingTransport{err: errors.New("dial tcp: connection refused")}},
	}

	err := n.Notify(context.Background(), Alert{Title: "t", Message: "m"})
	if err == nil {
		t.Fatal("expected error from failed ntfy push")
	}
	if strings.Contains(err.Error(), topic) {
		t.Fatalf("ntfy topic leaked into error: %q", err.Error())
	}
	if strings.Contains(err.Error(), rawURL) {
		t.Fatalf("full ntfy URL leaked into error: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "ntfy.example.com") {
		t.Errorf("masked error dropped the host, leaving it useless: %q", err.Error())
	}
}

func TestNewNtfyNotifier_RejectsNonHTTPS(t *testing.T) {
	for _, raw := range []string{
		"http://ntfy.example.com/secret-topic",
		"ftp://ntfy.example.com/secret-topic",
		"ntfy.example.com/secret-topic",
		"https://",
	} {
		t.Run(raw, func(t *testing.T) {
			n, err := NewNtfyNotifier(raw)
			if err == nil || n != nil {
				t.Fatalf("NewNtfyNotifier(%q) = %v, %v; want nil notifier and error", raw, n, err)
			}
			if strings.Contains(err.Error(), "secret-topic") {
				t.Errorf("ntfy topic leaked into construction error: %q", err.Error())
			}
		})
	}
}

func TestNewNtfyNotifier_AcceptsHTTPS(t *testing.T) {
	n, err := NewNtfyNotifier("HTTPS://ntfy.example.com/topic")
	if err != nil || n == nil {
		t.Fatalf("NewNtfyNotifier(https) = %v, %v; want notifier", n, err)
	}
}

func TestNtfyNotifier_RefusesRedirectToPlaintext(t *testing.T) {
	var plainHits atomic.Int32
	plain := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		plainHits.Add(1)
	}))
	defer plain.Close()
	tlsSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plain.URL+"/secret-topic", http.StatusPermanentRedirect)
	}))
	defer tlsSrv.Close()

	n, err := NewNtfyNotifier(tlsSrv.URL + "/secret-topic")
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	n.client.Transport = tlsSrv.Client().Transport

	if err := n.Notify(context.Background(), Alert{Title: "t", Message: "m"}); err == nil {
		t.Fatal("expected redirect to http to fail")
	}
	if got := plainHits.Load(); got != 0 {
		t.Fatalf("push followed redirect to plaintext server (%d hits)", got)
	}
}
