package app

import (
	"altune/go-api/internal/acquisition/adapters/ytdlp"
	"altune/go-api/internal/acquisition/adapters/ytmusic"
	"altune/go-api/internal/shared/config"
	"testing"

	acqPorts "altune/go-api/internal/acquisition/ports"
)

// TestAudioSourcesToggle reproduces the defect where the yt-dlp and ytmusic
// sources were wired unconditionally: before this change no config flag could
// skip either one, so disabling a single misbehaving provider meant a code
// change or taking down the whole audio store. Each source must now be gated
// independently by YTMUSIC_ENABLED / YTDLP_ENABLED, mirroring streamrip.
func TestAudioSourcesToggle(t *testing.T) {
	searcher := ytdlp.NewYtDlpAudioSearcher("", "", "")

	tests := []struct {
		name        string
		ytMusic     bool
		ytDlp       bool
		wantYtMusic bool
		wantYtDlp   bool
	}{
		{name: "both enabled (preserves current behavior)", ytMusic: true, ytDlp: true, wantYtMusic: true, wantYtDlp: true},
		{name: "ytmusic disabled", ytMusic: false, ytDlp: true, wantYtMusic: false, wantYtDlp: true},
		{name: "ytdlp disabled", ytMusic: true, ytDlp: false, wantYtMusic: true, wantYtDlp: false},
		{name: "both disabled", ytMusic: false, ytDlp: false, wantYtMusic: false, wantYtDlp: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &App{cfg: &config.Config{YtMusicEnabled: tt.ytMusic, YtDLPEnabled: tt.ytDlp}}

			names := sourceNames(a.audioSourcesFor(searcher))

			if got := containsSource(names, ytmusic.SourceName); got != tt.wantYtMusic {
				t.Errorf("ytmusic present = %v, want %v (sources=%v)", got, tt.wantYtMusic, names)
			}
			if got := containsSource(names, ytdlp.SourceName); got != tt.wantYtDlp {
				t.Errorf("ytdlp present = %v, want %v (sources=%v)", got, tt.wantYtDlp, names)
			}
		})
	}
}

func sourceNames(sources []acqPorts.AudioSource) []string {
	names := make([]string, 0, len(sources))
	for _, s := range sources {
		names = append(names, s.Name())
	}
	return names
}

func containsSource(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
