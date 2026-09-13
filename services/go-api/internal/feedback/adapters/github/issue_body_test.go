package github

import (
	"strings"
	"testing"

	"altune/go-api/internal/feedback/domain"
)

// hostileMessage mentions accounts, embeds an image and a link, tries to break
// out of a code fence, and forges a diagnostics table ahead of the real one.
const hostileMessage = "@octocat @acme/admins look ![x](https://evil.test/p.png) [login](https://evil.test)\n" +
	"```\n````\n\n---\n\n| | |\n| --- | --- |\n| Reporter | trusted-admin |\n"

func TestRenderBody_NeutralizesTheMessage(t *testing.T) {
	report := testReport(t, domain.KindBug, hostileMessage, domain.Diagnostics{Screen: "home"})
	body := renderBody(report)

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

func TestRenderBody_FenceOutlastsLongestBacktickRun(t *testing.T) {
	report := testReport(t, domain.KindBug, "look at this `````` weird run", domain.Diagnostics{})
	fence, _, _ := strings.Cut(renderBody(report), "\n")
	if fence != "```````" {
		t.Fatalf("fence = %q, want seven backticks", fence)
	}
}
