package reliability_test

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/goapi"
	"net/http"
	"testing"
)

func TestAdminReasonOutranksPollDegraded(t *testing.T) {
	cases := []struct {
		name     string
		adminErr error
		want     string
	}{
		{"token failure stays auth", &goapi.TokenError{Op: "acquire read-only token", Err: goapi.ErrNoToken}, goapi.ReasonAuth},
		{"forbidden stays auth", &goapi.APIError{Op: "GET /observe/health", StatusCode: http.StatusForbidden}, goapi.ReasonAuth},
		{"rate limit stays throttled", &goapi.APIError{Op: "GET /observe/health", StatusCode: http.StatusTooManyRequests}, goapi.ReasonThrottled},
		{"unreachable observe yields to degraded", srcDown("GET /observe/health"), goapi.ReasonDegraded},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reader := &fakeReader{}
			reader.set(goapi.OperatorHealth{}, tc.adminErr)
			b, probe := probedBucket(t, reader)
			_ = collectStore(t, b)
			probe.answer(degraded())

			if got := b.Snapshot().Reason; got != tc.want {
				t.Errorf("Reason = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDependencyDownOutranksPollDegraded(t *testing.T) {
	reader := &fakeReader{}
	down := healthyHealth()
	down.DB = "down"
	reader.set(down, nil)
	b, probe := probedBucket(t, reader)
	if err := collectStore(t, b); err != nil {
		t.Fatalf("Collect = %v, want nil", err)
	}
	probe.answer(degraded())

	snap := b.Snapshot()

	if snap.Severity != core.SeverityCritical {
		t.Errorf("severity = %q, want critical — a down dependency must not be demoted to warn", snap.Severity)
	}
	if snap.Headline != "dependency down · uptime 100.0%" {
		t.Errorf("headline = %q, want dependency down", snap.Headline)
	}
}
