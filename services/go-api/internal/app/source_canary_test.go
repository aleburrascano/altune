package app

import (
	"altune/go-api/internal/acquisition"
	"altune/go-api/internal/app/canary"
	"altune/go-api/internal/app/jobs"
	"testing"
)

func TestStartSourceCanary_DoesNotRegisterTheJobWhenEverySourceIsDisabled(t *testing.T) {
	a := &App{}
	a.startSourceCanary(canary.Probes{YtDlp: acquisition.NewYtDlpAudioSearcher("", "", ""), YtDlpAvailable: true, InnerTube: newYTMSearchClient()})

	if _, ok := a.jobs.SetEnabled(jobs.AcquisitionSourceCanary, false); ok {
		t.Fatal("job was registered despite every canaried source being disabled")
	}
}

func TestStartSourceCanary_DoesNotRegisterTheJobWhenNoProbeIsAvailable(t *testing.T) {
	a := &App{}
	a.startSourceCanary(canary.Probes{YtDlp: acquisition.NewYtDlpAudioSearcher("", "", ""), YtMusicEnabled: true, YtDlpEnabled: true})

	if _, ok := a.jobs.SetEnabled(jobs.AcquisitionSourceCanary, false); ok {
		t.Fatal("job was registered despite yt-dlp being unavailable and no InnerTube client")
	}
}

func TestStartSourceCanary_RegistersTheJobWhenYtDlpIsAvailable(t *testing.T) {
	a := &App{}

	a.startSourceCanary(canary.Probes{YtDlp: acquisition.NewYtDlpAudioSearcher("", "", ""), YtDlpAvailable: true, YtMusicEnabled: true, YtDlpEnabled: true})

	if _, ok := a.jobs.SetEnabled(jobs.AcquisitionSourceCanary, true); !ok {
		t.Fatal("job was not registered despite yt-dlp being reported available")
	}
}

func TestStartSourceCanary_RegistersTheJobForInnerTubeWhenYtDlpIsUnavailable(t *testing.T) {
	a := &App{}

	a.startSourceCanary(canary.Probes{InnerTube: newYTMSearchClient(), YtMusicEnabled: true, YtDlpEnabled: true})

	if _, ok := a.jobs.SetEnabled(jobs.AcquisitionSourceCanary, true); !ok {
		t.Fatal("job was not registered despite the InnerTube probe being available")
	}
}
