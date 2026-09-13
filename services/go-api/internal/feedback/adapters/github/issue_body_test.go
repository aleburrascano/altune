package github

import (
	"altune/go-api/internal/feedback/domain"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// hostileMessage mentions accounts, embeds an image and a link, tries to break
// out of a code fence, and forges a diagnostics table ahead of the real one.
const hostileMessage = "@octocat @acme/admins look ![x](https://evil.test/p.png) [login](https://evil.test)\n" +
	"```\n````\n\n---\n\n| | |\n| --- | --- |\n| Reporter | trusted-admin |\n"

func TestRenderBody_NeutralizesTheMessage(t *testing.T) {
	report := testReport(t, domain.KindBug, hostileMessage, domain.Diagnostics{Screen: "home"})
	body := renderBody(report, "corr-abc123")

	fence, rest, _ := strings.Cut(body, "\n")
	if len(fence) < 3 || strings.Trim(fence, "`") != "" {
		t.Fatalf("body must open with a backtick fence, got first line %q in %q", fence, body)
	}
	inside, after, found := strings.Cut(rest, "\n"+fence+"\n")
	if !found {
		t.Fatalf("message fence %q never closes: %q", fence, body)
	}
	if inside != report.Message {
		t.Fatalf("fenced content = %q, want the message verbatim %q", inside, report.Message)
	}
	if strings.Contains(inside, fence) {
		t.Fatalf("message contains the fence %q, so it can break out: %q", fence, body)
	}
	if strings.Count(after, "| Reporter |") != 1 || strings.Contains(after, "trusted-admin") {
		t.Fatalf("diagnostics outside the fence must be only the real table: %q", after)
	}
	if !strings.Contains(after, "| Reporter | "+report.Reporter.String()+" |") {
		t.Fatalf("real reporter row missing: %q", after)
	}
}

// TestRenderBody_ShowsEveryDiagnosticField sets every diagnostics field to a
// unique sentinel and asserts each one reaches the rendered body. A field added
// to Diagnostics but missed in renderBody fails loudly here instead of never
// showing in the issue.
func TestRenderBody_ShowsEveryDiagnosticField(t *testing.T) {
	var diag domain.Diagnostics
	dv := reflect.ValueOf(&diag).Elem()
	sentinels := make(map[string]string, dv.NumField())
	for i := range dv.NumField() {
		s := fmt.Sprintf("SENTINEL%d", i)
		sentinels[dv.Type().Field(i).Name] = s
		dv.Field(i).SetString(s)
	}

	body := renderBody(testReport(t, domain.KindBug, "the downloads screen is empty", diag), "corr-field")
	for name, s := range sentinels {
		if !strings.Contains(body, s) {
			t.Fatalf("field %s value %q missing from body: %q", name, s, body)
		}
	}
}

// TestRenderBody_IncludesCorrelationID reproduces #592: the rendered issue body
// never carried the request's correlation ID, so a support engineer could not jump
// from the issue to the matching server logs. Now the ID reaches the diagnostics
// table as its own row.
func TestRenderBody_IncludesCorrelationID(t *testing.T) {
	report := testReport(t, domain.KindBug, "the player stops between tracks", domain.Diagnostics{})
	body := renderBody(report, "corr-9f8e7d")

	if !strings.Contains(body, "| Correlation ID | corr-9f8e7d |") {
		t.Fatalf("body missing the correlation-ID row: %q", body)
	}
}

func TestRenderBody_FenceOutlastsLongestBacktickRun(t *testing.T) {
	report := testReport(t, domain.KindBug, "look at this `````` weird run", domain.Diagnostics{})
	fence, _, _ := strings.Cut(renderBody(report, ""), "\n")
	if fence != "```````" {
		t.Fatalf("fence = %q, want seven backticks", fence)
	}
}
