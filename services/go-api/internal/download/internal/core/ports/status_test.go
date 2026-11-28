package ports_test

import (
	"altune/go-api/internal/download/internal/core/ports"
	"testing"
)

func TestAcquisitionVerification_ToolsPresentNeedsEveryTool(t *testing.T) {
	all := ports.Verification{Ffprobe: true, Ffmpeg: true, Fpcalc: true, YtDlp: true}
	if !all.ToolsPresent() {
		t.Fatalf("%+v.ToolsPresent() = false, want true with every tool present", all)
	}
	missing := map[string]func(*ports.Verification){
		"ffprobe": func(v *ports.Verification) { v.Ffprobe = false },
		"ffmpeg":  func(v *ports.Verification) { v.Ffmpeg = false },
		"fpcalc":  func(v *ports.Verification) { v.Fpcalc = false },
		"yt-dlp":  func(v *ports.Verification) { v.YtDlp = false },
	}
	for tool, drop := range missing {
		v := all
		drop(&v)
		if v.ToolsPresent() {
			t.Errorf("without %s: ToolsPresent() = true, want false", tool)
		}
	}
}
