package service

import (
	"altune/go-api/internal/acquisition/ports"
	"testing"
)

// TestAcquisitionVerification_FullyArmed_RequiresYtDlp reproduces the defect:
// when yt-dlp is unavailable but ffprobe/ffmpeg/fpcalc are present, the
// verification must report as degraded so WithVerificationStatus logs the
// startup warning. Before yt-dlp joined the verification, FullyArmed reported
// armed and no warning ever fired.
func TestAcquisitionVerification_FullyArmed_RequiresYtDlp(t *testing.T) {
	armed := ports.AcquisitionVerification{Ffprobe: true, Ffmpeg: true, Fpcalc: true, YtDlp: true, Streamrip: true}
	if !armed.FullyArmed() {
		t.Error("FullyArmed() = false with every tool present, want true")
	}

	missingYtDlp := ports.AcquisitionVerification{Ffprobe: true, Ffmpeg: true, Fpcalc: true, Streamrip: true}
	if missingYtDlp.FullyArmed() {
		t.Error("FullyArmed() = true with yt-dlp unavailable; the startup warning never fires (defect)")
	}
}

// TestAcquisitionVerification_FullyArmed_RequiresStreamrip: a configured but
// unrunnable streamrip binary must report degraded, like every other tool.
func TestAcquisitionVerification_FullyArmed_RequiresStreamrip(t *testing.T) {
	missingStreamrip := ports.AcquisitionVerification{Ffprobe: true, Ffmpeg: true, Fpcalc: true, YtDlp: true}
	if missingStreamrip.FullyArmed() {
		t.Error("FullyArmed() = true with streamrip unavailable, want degraded")
	}
}
