package domain_test

import (
	"altune/go-api/internal/download/internal/domain"
	"testing"
)

func TestAcquisitionVerification_ToolsPresentNeedsEveryTool(t *testing.T) {
	all := domain.Verification{Ffprobe: true, Ffmpeg: true, Fpcalc: true, YtDlp: true}
	if !all.ToolsPresent() {
		t.Fatalf("%+v.ToolsPresent() = false, want true with every tool present", all)
	}
	missing := map[string]func(*domain.Verification){
		"ffprobe": func(v *domain.Verification) { v.Ffprobe = false },
		"ffmpeg":  func(v *domain.Verification) { v.Ffmpeg = false },
		"fpcalc":  func(v *domain.Verification) { v.Fpcalc = false },
		"yt-dlp":  func(v *domain.Verification) { v.YtDlp = false },
	}
	for tool, drop := range missing {
		v := all
		drop(&v)
		if v.ToolsPresent() {
			t.Errorf("without %s: ToolsPresent() = true, want false", tool)
		}
	}
}
