package alert

import (
	"context"
	"errors"
	"net/http"
	"strings"
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
