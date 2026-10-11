package acquisitionbridge

import (
	"altune/go-api/internal/acquisition/ports"
	"encoding/json"
	"strings"
	"testing"
)

type stubAcquisitionReader struct {
	calls  int
	status func(call int) ports.AcquisitionStatus
}

func (s *stubAcquisitionReader) Status() ports.AcquisitionStatus {
	s.calls++
	return s.status(s.calls)
}

func TestReadAcquisitionStatusNilReaderGivesEmptyLists(t *testing.T) {
	raw, err := json.Marshal(ReadAcquisitionStatus(nil))
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	for _, want := range []string{`"jobs":[]`, `"recent":[]`} {
		if !strings.Contains(s, want) {
			t.Fatalf("%s missing from %s", want, s)
		}
	}
}

func TestReadAcquisitionStatusOneJobGivesOneDTO(t *testing.T) {
	r := &stubAcquisitionReader{status: func(int) ports.AcquisitionStatus {
		return ports.AcquisitionStatus{ActiveJobs: []ports.JobRecord{{TrackID: "t1", State: "running"}}}
	}}
	got := ReadAcquisitionStatus(r)
	if len(got.ActiveJobs) != 1 || got.ActiveJobs[0].TrackID != "t1" || got.ActiveJobs[0].State != "running" {
		t.Fatalf("jobs = %+v", got.ActiveJobs)
	}
	if got.Recent == nil || len(got.Recent) != 0 {
		t.Fatalf("recent = %#v", got.Recent)
	}
}

func TestRedactSourceURL(t *testing.T) {
	cases := map[string]string{
		"https://u:p@host.example/a/b?token=abc": "https://host.example/a/b",
		"http://[::1":                            "",
	}
	for in, want := range cases {
		if got := redactSourceURL(in); got != want {
			t.Errorf("redactSourceURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestReadAcquisitionStatusDoesNotCache(t *testing.T) {
	r := &stubAcquisitionReader{status: func(call int) ports.AcquisitionStatus {
		return ports.AcquisitionStatus{InFlight: call}
	}}
	if got := ReadAcquisitionStatus(r).InFlight; got != 1 {
		t.Fatalf("first InFlight = %d", got)
	}
	if got := ReadAcquisitionStatus(r).InFlight; got != 2 {
		t.Fatalf("second InFlight = %d", got)
	}
}

func TestReadAcquisitionStatusVerificationNamesOnlyLiveTools(t *testing.T) {
	r := &stubAcquisitionReader{status: func(int) ports.AcquisitionStatus {
		return ports.AcquisitionStatus{Verification: ports.AcquisitionVerification{Ffprobe: true, Ffmpeg: true, Fpcalc: true, YtDlp: true}}
	}}
	raw, err := json.Marshal(ReadAcquisitionStatus(r).Verification)
	if err != nil {
		t.Fatal(err)
	}
	var keys map[string]bool
	if err := json.Unmarshal(raw, &keys); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"ffprobe": true, "ffmpeg": true, "fpcalc": true, "yt_dlp": true}
	if len(keys) != len(want) {
		t.Fatalf("verification = %s, want exactly %v", raw, want)
	}
	for k, v := range want {
		if keys[k] != v {
			t.Fatalf("verification = %s, want %s=%v", raw, k, v)
		}
	}
}
