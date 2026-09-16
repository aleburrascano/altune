package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

func TestClampSinceDays_AboveWindowClampsWithNotice(t *testing.T) {
	var notice bytes.Buffer

	clamped := clampSinceDays(&notice, aggregateRetentionDays+909)

	if clamped != aggregateRetentionDays {
		t.Errorf("clamped = %d, want %d (the retention ceiling)", clamped, aggregateRetentionDays)
	}
	if !strings.Contains(notice.String(), fmt.Sprintf("clamping to %d", aggregateRetentionDays)) {
		t.Errorf("notice did not announce the clamp:\n%s", notice.String())
	}
}

func TestClampSinceDays_AtWindowIsUntouchedAndSilent(t *testing.T) {
	var notice bytes.Buffer

	clamped := clampSinceDays(&notice, aggregateRetentionDays)

	if clamped != aggregateRetentionDays {
		t.Errorf("clamped = %d, want %d unchanged", clamped, aggregateRetentionDays)
	}
	if notice.Len() != 0 {
		t.Errorf("a within-window value must not print a notice, got:\n%s", notice.String())
	}
}

func TestClampSinceDays_WithinWindowIsUntouchedAndSilent(t *testing.T) {
	var notice bytes.Buffer

	clamped := clampSinceDays(&notice, 30)

	if clamped != 30 {
		t.Errorf("clamped = %d, want 30 unchanged", clamped)
	}
	if notice.Len() != 0 {
		t.Errorf("a within-window value must not print a notice, got:\n%s", notice.String())
	}
}
