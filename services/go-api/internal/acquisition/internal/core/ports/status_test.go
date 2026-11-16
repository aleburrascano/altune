package ports_test

import (
	"altune/go-api/internal/acquisition"
	"testing"
)

func TestAcquisitionVerification_ToolsPresentNeedsEveryTool(t *testing.T) {
	all := acquisition.AcquisitionVerification{Ffprobe: true, Ffmpeg: true, Fpcalc: true, YtDlp: true}
	if !all.ToolsPresent() {
		t.Fatalf("%+v.ToolsPresent() = false, want true with every tool present", all)
	}
	missing := map[string]func(*acquisition.AcquisitionVerification){
		"ffprobe": func(v *acquisition.AcquisitionVerification) { v.Ffprobe = false },
		"ffmpeg":  func(v *acquisition.AcquisitionVerification) { v.Ffmpeg = false },
		"fpcalc":  func(v *acquisition.AcquisitionVerification) { v.Fpcalc = false },
		"yt-dlp":  func(v *acquisition.AcquisitionVerification) { v.YtDlp = false },
	}
	for tool, drop := range missing {
		v := all
		drop(&v)
		if v.ToolsPresent() {
			t.Errorf("without %s: ToolsPresent() = true, want false", tool)
		}
	}
}
