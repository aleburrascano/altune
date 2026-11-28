package notifier_test

import (
	"altune/go-api/internal/platform/monitoring/internal/adapters/notifier"
	"altune/go-api/internal/platform/monitoring/internal/domain"
	"altune/go-api/internal/platform/monitoring/internal/ports"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

const testResendKey = "re_TESTKEY_never_in_output"

type sentEmail struct {
	method string
	url    string
	header http.Header
	body   map[string]any
}

type resendStub struct {
	srv     *httptest.Server
	status  int
	reply   string
	sent    []sentEmail
	targets []string
}

func newResendStub(t *testing.T, status int, reply string) *resendStub {
	t.Helper()
	stub := &resendStub{status: status, reply: reply}
	stub.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("request body is not JSON: %q", raw)
		}
		stub.sent = append(stub.sent, sentEmail{method: r.Method, url: r.URL.String(), header: r.Header.Clone(), body: body})
		w.WriteHeader(stub.status)
		_, _ = io.WriteString(w, stub.reply)
	}))
	t.Cleanup(stub.srv.Close)
	return stub
}

func (s *resendStub) RoundTrip(r *http.Request) (*http.Response, error) {
	s.targets = append(s.targets, r.URL.String())
	local, err := url.Parse(s.srv.URL)
	if err != nil {
		return nil, err
	}
	routed := r.Clone(r.Context())
	routed.URL.Scheme, routed.URL.Host = local.Scheme, local.Host
	return http.DefaultTransport.RoundTrip(routed)
}

func (s *resendStub) notifier() ports.AlertNotifier {
	return notifier.NewEmailNotifier(testResendKey, "ops@example.com", s)
}

func TestEmailNotifier_PostsTheAlertToResendAsOneEmail(t *testing.T) {
	stub := newResendStub(t, http.StatusOK, `{"id":"49a3999c-0ce1-4ea6-ab68-afcd6dc2e794"}`)

	err := stub.notifier().Notify(context.Background(), domain.Alert{
		Title:    "altune dependency down",
		Message:  "dependencies down: db",
		Severity: domain.SeverityWatch,
	})
	if err != nil {
		t.Fatalf("Notify = %v, want nil", err)
	}
	if want := []string{"https://api.resend.com/emails"}; !reflect.DeepEqual(stub.targets, want) {
		t.Fatalf("requests went to %v, want %v", stub.targets, want)
	}
	got := stub.sent[0]
	if got.method != http.MethodPost || got.url != "/emails" {
		t.Errorf("request = %s %s, want POST /emails", got.method, got.url)
	}
	if got.header.Get("Authorization") != "Bearer "+testResendKey {
		t.Error("Authorization header is not Bearer and the API key")
	}
	if ct := got.header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	want := map[string]any{
		"from":    "altune alerts <onboarding@resend.dev>",
		"to":      "ops@example.com",
		"subject": "altune dependency down",
		"text":    "dependencies down: db\n\nSeverity: signal",
	}
	if !reflect.DeepEqual(got.body, want) {
		t.Errorf("body = %v, want %v", got.body, want)
	}
}

func TestEmailNotifier_NamesEachSeverityInWords(t *testing.T) {
	cases := []struct {
		severity domain.Severity
		want     string
	}{
		{severity: domain.SeverityActNow, want: "m\n\nSeverity: fix"},
		{severity: domain.SeverityRecord, want: "m\n\nSeverity: log"},
		{severity: domain.SeverityWatch, want: "m\n\nSeverity: signal"},
		{severity: domain.Severity(7), want: "m\n\nSeverity: unknown (7)"},
	}
	for _, tc := range cases {
		stub := newResendStub(t, http.StatusOK, `{}`)

		if err := stub.notifier().Notify(context.Background(), domain.Alert{Title: "t", Message: "m", Severity: tc.severity}); err != nil {
			t.Fatalf("Notify = %v, want nil", err)
		}
		if got := stub.sent[0].body["text"]; got != tc.want {
			t.Errorf("text for severity %d = %q, want %q", tc.severity, got, tc.want)
		}
	}
}

func TestEmailNotifier_ARejectionNamesResendsReasonWithoutTheKey(t *testing.T) {
	cases := []struct {
		name   string
		status int
		reply  string
		want   string
	}{
		{
			name:   "resend explains the refusal",
			status: http.StatusForbidden,
			reply:  `{"statusCode":403,"name":"validation_error","message":"You can only send testing emails to your own email address"}`,
			want:   "send alert email: status 403: validation_error: You can only send testing emails to your own email address",
		},
		{
			name:   "resend echoes the key back",
			status: http.StatusUnauthorized,
			reply:  `{"statusCode":401,"name":"validation_error","message":"API key ` + testResendKey + ` is invalid"}`,
			want:   "send alert email: status 401: validation_error: API key [redacted] is invalid",
		},
		{
			name:   "server fails with a body that is not JSON",
			status: http.StatusBadGateway,
			reply:  "<html>bad gateway " + testResendKey + "</html>",
			want:   "send alert email: status 502",
		},
		{
			name:   "server fails with JSON that has no message",
			status: http.StatusInternalServerError,
			reply:  `{"name":"internal_server_error"}`,
			want:   "send alert email: status 500",
		},
		{
			name:   "a status past the two hundreds is not a delivery",
			status: http.StatusMultipleChoices,
			reply:  "",
			want:   "send alert email: status 300",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := newResendStub(t, tc.status, tc.reply)

			err := stub.notifier().Notify(context.Background(), domain.Alert{Title: "t", Message: "m"})

			if err == nil {
				t.Fatalf("Notify on %d = nil, want an error", tc.status)
			}
			if strings.Contains(err.Error(), "TESTKEY") {
				t.Fatal("error carries the Resend API key")
			}
			if err.Error() != tc.want {
				t.Errorf("error = %q, want %q", err.Error(), tc.want)
			}
		})
	}
}

func TestEmailNotifier_ARejectionReadsWholeWhenTheKeyIsEmpty(t *testing.T) {
	stub := newResendStub(t, http.StatusUnauthorized, `{"name":"missing_api_key","message":"Missing API key in the authorization header"}`)

	err := notifier.NewEmailNotifier("", "ops@example.com", stub).Notify(context.Background(), domain.Alert{})

	want := "send alert email: status 401: missing_api_key: Missing API key in the authorization header"
	if err == nil || err.Error() != want {
		t.Errorf("error = %v, want %q", err, want)
	}
}

func TestEmailNotifier_AnyTwoHundredStatusIsADelivery(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusAccepted, 299} {
		stub := newResendStub(t, status, "")

		if err := stub.notifier().Notify(context.Background(), domain.Alert{}); err != nil {
			t.Errorf("Notify on %d = %v, want nil", status, err)
		}
	}
}

func TestEmailNotifier_AnOverlongRejectionIsCutBeforeItIsRead(t *testing.T) {
	reply := `{"name":"validation_error","message":"` + strings.Repeat("x", 8<<10) + `"}`
	stub := newResendStub(t, http.StatusUnprocessableEntity, reply)

	err := stub.notifier().Notify(context.Background(), domain.Alert{})

	if err == nil || err.Error() != "send alert email: status 422" {
		t.Errorf("error = %v, want the bare status once the reply is cut", err)
	}
}

func TestEmailNotifier_AnUnreachableResendFailsWithoutTheKey(t *testing.T) {
	failing := roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("dial tcp: connection refused")
	})

	err := notifier.NewEmailNotifier(testResendKey, "ops@example.com", failing).Notify(context.Background(), domain.Alert{})

	if err == nil {
		t.Fatal("Notify with no connection = nil, want an error")
	}
	if !strings.HasPrefix(err.Error(), "send alert email: ") || !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("error = %q, want it to say the send failed and why", err)
	}
	if strings.Contains(err.Error(), "TESTKEY") {
		t.Error("error carries the Resend API key")
	}
}

func TestEmailNotifier_GivesUpOnAResendThatNeverAnswersAfterTenSeconds(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hanging := roundTripFunc(func(r *http.Request) (*http.Response, error) {
			<-r.Context().Done()
			return nil, r.Context().Err()
		})
		start := time.Now()

		err := notifier.NewEmailNotifier(testResendKey, "ops@example.com", hanging).Notify(context.Background(), domain.Alert{})

		if err == nil {
			t.Fatal("Notify against a silent Resend = nil, want an error")
		}
		if waited := time.Since(start); waited != 10*time.Second {
			t.Errorf("gave up after %s, want 10s", waited)
		}
	})
}

func TestEmailNotifier_RetriesOfOneFiringShareAnIdempotencyKeyAndANewFiringGetsANewOne(t *testing.T) {
	stub := newResendStub(t, http.StatusOK, `{}`)
	firedAt := time.Date(2026, 11, 20, 9, 30, 0, 0, time.UTC)
	sends := []domain.Alert{
		{Key: "dependency_down", FiredAt: firedAt, Message: "dependencies down: db"},
		{Key: "dependency_down", FiredAt: firedAt, Message: "dependencies down: db"},
		{Key: "dependency_down", FiredAt: firedAt.In(time.FixedZone("CET", 3600)), Message: "dependencies down: db"},
		{Key: "dependency_down", FiredAt: firedAt.Add(time.Nanosecond), Message: "dependencies down: db"},
		{Key: "coverage_zero_result", FiredAt: firedAt, Message: "dependencies down: db"},
	}
	for _, a := range sends {
		if err := stub.notifier().Notify(context.Background(), a); err != nil {
			t.Fatalf("Notify = %v, want nil", err)
		}
	}
	keys := make([]string, len(stub.sent))
	for i, sent := range stub.sent {
		keys[i] = sent.header.Get("Idempotency-Key")
	}

	if keys[0] == "" || len(keys[0]) > 256 {
		t.Fatalf("Idempotency-Key = %q, want a key of at most 256 characters", keys[0])
	}
	if keys[1] != keys[0] {
		t.Errorf("a retry of one firing sent key %q, the first send %q", keys[1], keys[0])
	}
	if keys[2] != keys[0] {
		t.Errorf("the same firing time in another zone sent key %q, want %q", keys[2], keys[0])
	}
	if keys[3] == keys[0] {
		t.Error("a firing a nanosecond later reused the earlier firing's key")
	}
	if keys[4] == keys[0] {
		t.Error("another condition firing at the same time reused the key")
	}
}

func TestEmailNotifier_AnAlertThatNamesNoFiringSendsNoIdempotencyKey(t *testing.T) {
	cases := map[string]domain.Alert{
		"no condition key": {FiredAt: time.Date(2026, 11, 20, 9, 30, 0, 0, time.UTC)},
		"no firing time":   {Key: "dependency_down"},
	}
	for name, a := range cases {
		t.Run(name, func(t *testing.T) {
			stub := newResendStub(t, http.StatusOK, `{}`)

			if err := stub.notifier().Notify(context.Background(), a); err != nil {
				t.Fatalf("Notify = %v, want nil", err)
			}
			if _, sent := stub.sent[0].header["Idempotency-Key"]; sent {
				t.Error("sent an Idempotency-Key that every such alert would share, so Resend would drop the later ones")
			}
		})
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
