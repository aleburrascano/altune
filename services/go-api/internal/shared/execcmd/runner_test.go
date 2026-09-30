package execcmd

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestRunWithTimeout_CapsCapturedOutput(t *testing.T) {
	over := MaxCaptureBytes + 4096
	stdout, _, err := RunWithTimeout(
		context.Background(), 30*time.Second,
		"sh", "-c", fmt.Sprintf("head -c %d /dev/zero", over),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stdout) > MaxCaptureBytes {
		t.Fatalf("stdout not capped: got %d bytes, cap %d", len(stdout), MaxCaptureBytes)
	}
}

func TestRunCapture_FlagsOutputPastTheCap(t *testing.T) {
	over := MaxCaptureBytes + 4096
	stdout, _, truncated, err := RunCapture(context.Background(), "sh", "-c", fmt.Sprintf("head -c %d /dev/zero", over))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !truncated {
		t.Fatal("truncated = false for output past the cap")
	}
	if len(stdout) != MaxCaptureBytes {
		t.Fatalf("stdout length %d, want %d", len(stdout), MaxCaptureBytes)
	}
}

func TestRunCapture_DoesNotFlagOutputUnderTheCap(t *testing.T) {
	_, _, truncated, err := RunCapture(context.Background(), "sh", "-c", "echo hi")
	if err != nil || truncated {
		t.Fatalf("got truncated=%v err=%v, want false nil", truncated, err)
	}
}

func TestRun_StillReturnsCutOutputWithoutError(t *testing.T) {
	over := MaxCaptureBytes + 4096
	stdout, _, err := Run(context.Background(), "sh", "-c", fmt.Sprintf("head -c %d /dev/zero", over))
	if err != nil || len(stdout) != MaxCaptureBytes {
		t.Fatalf("got len %d err %v, want %d nil", len(stdout), err, MaxCaptureBytes)
	}
}

func TestRunCapture_DoesNotFlagStdoutOfExactlyTheCap(t *testing.T) {
	stdout, _, truncated, err := RunCapture(context.Background(), "sh", "-c", fmt.Sprintf("head -c %d /dev/zero", MaxCaptureBytes))
	if err != nil || truncated {
		t.Fatalf("got truncated=%v err=%v, want false nil", truncated, err)
	}
	if len(stdout) != MaxCaptureBytes {
		t.Fatalf("stdout length %d, want %d", len(stdout), MaxCaptureBytes)
	}
}

func TestRunCapture_DoesNotFlagStdoutWhenOnlyStderrOverflows(t *testing.T) {
	over := MaxCaptureBytes + 4096
	stdout, stderr, truncated, err := RunCapture(context.Background(), "sh", "-c", fmt.Sprintf("echo hi; head -c %d /dev/zero 1>&2", over))
	if err != nil || truncated {
		t.Fatalf("got truncated=%v err=%v, want false nil", truncated, err)
	}
	if stdout != "hi\n" || len(stderr) != MaxCaptureBytes {
		t.Fatalf("got stdout %q stderr len %d, want %q and %d", stdout, len(stderr), "hi\n", MaxCaptureBytes)
	}
}
