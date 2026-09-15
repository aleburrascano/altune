package providers

import (
	"altune/go-api/internal/feedback/domain"
	"fmt"
	"os"
	"path/filepath"
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

// TestRenderBody_ReporterIdentityMatchesShippedContract reproduces #1117: the
// issue body published the reporter UUID while .env.example promised "reports
// carry no reporter identity". The body must carry exactly the opaque UUID under
// reporterIdentityRow, and the shipped .env.example must name that constant and
// no longer deny publishing identity.
func TestRenderBody_ReporterIdentityMatchesShippedContract(t *testing.T) {
	report := testReport(t, domain.KindBug, "the queue forgets its order", domain.Diagnostics{})
	body := renderBody(report, "corr-1117")

	row := "| " + reporterIdentityRow + " | " + report.Reporter.String() + " |"
	if strings.Count(body, row) != 1 {
		t.Fatalf("body must publish the reporter UUID exactly once as %q: %q", row, body)
	}

	envExample, err := os.ReadFile(filepath.Join("..", "..", "..", "..", ".env.example"))
	if err != nil {
		t.Fatalf("read .env.example: %v", err)
	}
	doc := string(envExample)
	if strings.Contains(doc, "carry no reporter identity") {
		t.Fatal(".env.example still claims reports carry no reporter identity")
	}
	if !strings.Contains(doc, "`reporterIdentityRow`") {
		t.Fatal(".env.example must name reporterIdentityRow as the reporter-identity contract")
	}
}

func TestRenderBody_FenceOutlastsLongestBacktickRun(t *testing.T) {
	report := testReport(t, domain.KindBug, "look at this `````` weird run", domain.Diagnostics{})
	fence, _, _ := strings.Cut(renderBody(report, ""), "\n")
	if fence != "```````" {
		t.Fatalf("fence = %q, want seven backticks", fence)
	}
}

// hostileDiagnostic mentions a team, plants a tracking image and a link, and
// embeds raw HTML: everything a diagnostics cell must show only literally. It
// fits the domain's diagnostics length cap, so it reaches the table untruncated.
const hostileDiagnostic = "@acme/admins ![x](https://e.test/t.png) [a](https://e.test) <b>"

// TestRenderBody_NeutralizesDiagnostics reproduces #1107: diagnostics come
// verbatim from the client and only had pipes escaped, so a value rendered a
// live @mention, image, link, or HTML inside the issue table. Each value must
// now reach the table as one inline code span.
func TestRenderBody_NeutralizesDiagnostics(t *testing.T) {
	cases := map[string]struct {
		diag domain.Diagnostics
		row  string
	}{
		"app version": {domain.Diagnostics{AppVersion: hostileDiagnostic}, "App"},
		"platform":    {domain.Diagnostics{Platform: hostileDiagnostic}, "Platform"},
		"os version":  {domain.Diagnostics{OSVersion: hostileDiagnostic}, "Platform"},
		"screen":      {domain.Diagnostics{Screen: hostileDiagnostic}, "Screen"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			body := renderBody(testReport(t, domain.KindBug, "the queue forgets its order", tc.diag), "corr-1107")
			want := "| " + tc.row + " | `" + hostileDiagnostic + "` |\n"
			if !strings.Contains(body, want) {
				t.Fatalf("body must render the value as one code span %q: %q", want, body)
			}
		})
	}
}

func TestInlineCode_OutlastsBacktickRunsAndKeepsEdges(t *testing.T) {
	cases := []struct{ in, want string }{
		{"ios", "`ios`"},
		{"a ` b", "``a ` b``"},
		{"a `` b", "```a `` b```"},
		{"`x", "`` `x ``"},
		{"x`", "`` x` ``"},
		{" x ", "`  x  `"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := inlineCode(tc.in); got != tc.want {
			t.Fatalf("inlineCode(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestPlainTitle_NeutralizesMentions(t *testing.T) {
	got := plainTitle("[bug] @octocat @acme/security-team mail me@x.test")
	if strings.Contains(got, "@") {
		t.Fatalf("plainTitle left a mentionable @: %q", got)
	}
	if want := "[bug] \uFF20octocat \uFF20acme/security-team mail me\uFF20x.test"; got != want {
		t.Fatalf("plainTitle = %q, want %q", got, want)
	}
}
