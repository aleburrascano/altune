package main

import (
	"bytes"
	"strings"
	"testing"
)

const auditTSV = "id-1\tRollacoasta\tprettifun\thttps://youtube.com/watch?v=C5xk0CyIDyo\tprettifun - Rollacoasta (Instrumental) [100% Accurate]\n" +
	"id-2\tSPEED DEMON\tLucy Bedroque\thttps://soundcloud.com/maisonnnn/lucy-bedroque-speed-demon\tLucy Bedroque - SPEED DEMON (Instrumental)\n" +
	"id-3\tClean Song\tSomeone\thttps://youtube.com/watch?v=x\tSomeone - Clean Song\n" +
	"id-4\tNo Title\tSomeone\thttps://youtube.com/watch?v=y\t\n"

func TestRunAuditQualifiers_FlagsInstrumentalsAndSkipsCleanLines(t *testing.T) {
	var out bytes.Buffer

	code := runAuditQualifiers(strings.NewReader(auditTSV), &out)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	got := out.String()
	for _, id := range []string{"id-1\t", "id-2\t"} {
		if !strings.Contains(got, id) {
			t.Errorf("output missing flagged %q:\n%s", id, got)
		}
	}
	for _, id := range []string{"id-3", "id-4"} {
		if strings.Contains(got, id) {
			t.Errorf("output flagged %q, want it left out:\n%s", id, got)
		}
	}
	if !strings.HasSuffix(got, "flagged 2 of 4\n") {
		t.Errorf("output missing count line:\n%s", got)
	}
}

func TestRunAuditQualifiers_MalformedLineExitsNonZero(t *testing.T) {
	var out bytes.Buffer

	if code := runAuditQualifiers(strings.NewReader("only\ttwo\n"), &out); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
}
