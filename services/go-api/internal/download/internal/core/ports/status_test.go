package ports_test

import (
	"altune/go-api/internal/download"
	"testing"
)

func TestAcquisitionVerification_ToolsPresentNeedsEveryTool(t *testing.T) {
	all := download.Verification{Ffprobe: true, Ffmpeg: true, Fpcalc: true, YtDlp: true}
	if !all.ToolsPresent() {
		t.Fatalf("%+v.ToolsPresent() = false, want true with every tool present", all)
	}
	missing := map[string]func(*download.Verification){
		"ffprobe": func(v *download.Verification) { v.Ffprobe = false },
		"ffmpeg":  func(v *download.Verification) { v.Ffmpeg = false },
		"fpcalc":  func(v *download.Verification) { v.Fpcalc = false },
		"yt-dlp":  func(v *download.Verification) { v.YtDlp = false },
	}
	for tool, drop := range missing {
		v := all
		drop(&v)
		if v.ToolsPresent() {
			t.Errorf("without %s: ToolsPresent() = true, want false", tool)
		}
	}
}
