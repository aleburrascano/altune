package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"altune/go-api/internal/acquisition/ports"
)

type stubIdentifier struct {
	match   ports.RecordingMatch
	err     error
	cluster []string
	calls   int
}

func (s *stubIdentifier) Identify(context.Context, string) (ports.RecordingMatch, error) {
	s.calls++
	return s.match, s.err
}

func (s *stubIdentifier) AcoustIDsFor(context.Context, string) ([]string, error) {
	return s.cluster, nil
}

func downloadContext(mbid string, cluster []string) *AcquisitionContext {
	return &AcquisitionContext{
		Track:    TrackRef{Title: "Sunglasses at Night", Artist: "Corey Hart"},
		Identity: ports.RecordingIdentity{MBID: mbid, AcoustIDs: cluster},
		Ranked: []ports.AudioCandidate{
			{URL: "https://youtube.com/watch?v=first000000"},
			{URL: "https://youtube.com/watch?v=second00000"},
		},
	}
}

func TestDownloadStep_RejectsAudioOutsideTheExpectedCluster(t *testing.T) {
	identifier := &stubIdentifier{
		match:   ports.RecordingMatch{AcoustID: "ac-acoustic", MBIDs: []string{"mb-acoustic"}},
		cluster: []string{"ac-master-a", "ac-master-b"},
	}
	step := NewDownloadStep(&fileWritingSearcher{writeFile: true}, WithDownloadIdentifier(identifier))
	ac := downloadContext("mb-master", []string{"ac-master-a", "ac-master-b"})

	if err := step.Execute(context.Background(), ac); err == nil {
		t.Fatal("expected every candidate rejected: the audio is a different recording")
	}
	if ac.TempPath != "" {
		t.Errorf("TempPath must stay empty when the fingerprint rejects, got %q", ac.TempPath)
	}
}

func TestDownloadStep_AcceptsAudioInsideTheExpectedCluster(t *testing.T) {
	identifier := &stubIdentifier{
		match: ports.RecordingMatch{AcoustID: "ac-master-b", MBIDs: []string{"mb-some-other-release"}},
	}
	step := NewDownloadStep(&fileWritingSearcher{writeFile: true}, WithDownloadIdentifier(identifier))
	ac := downloadContext("mb-master", []string{"ac-master-a", "ac-master-b"})

	if err := step.Execute(context.Background(), ac); err != nil {
		t.Fatalf("the Don't Stop the Music case: MBIDs need not intersect, the cluster settles it — got %v", err)
	}
	defer os.RemoveAll(filepath.Dir(ac.TempPath))

	if !ac.IdentityVerified {
		t.Error("a cluster hit is the strongest claim available and must mark the track verified")
	}
}

func TestDownloadStep_NeverRejectsWithoutAnExpectedCluster(t *testing.T) {
	identifier := &stubIdentifier{
		match: ports.RecordingMatch{AcoustID: "ac-unrelated", MBIDs: []string{"mb-unrelated"}},
	}
	step := NewDownloadStep(&fileWritingSearcher{writeFile: true}, WithDownloadIdentifier(identifier))
	ac := downloadContext("mb-master", nil)

	if err := step.Execute(context.Background(), ac); err != nil {
		t.Fatalf("with no ground truth there is nothing to reject against; the long tail must stay acquirable: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(ac.TempPath))

	if ac.IdentityVerified {
		t.Error("accepting without evidence must not claim verification")
	}
}

func TestDownloadStep_UnknownAudioIsAccepted(t *testing.T) {
	identifier := &stubIdentifier{cluster: []string{"ac-master"}}
	step := NewDownloadStep(&fileWritingSearcher{writeFile: true}, WithDownloadIdentifier(identifier))
	ac := downloadContext("mb-master", []string{"ac-master"})

	if err := step.Execute(context.Background(), ac); err != nil {
		t.Fatalf("AcoustID coverage is crowd-sourced; unknown audio must be accepted: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(ac.TempPath))
}

func TestDownloadStep_IdentifierErrorIsFailOpen(t *testing.T) {
	identifier := &stubIdentifier{err: errors.New("acoustid down"), cluster: []string{"ac-master"}}
	step := NewDownloadStep(&fileWritingSearcher{writeFile: true}, WithDownloadIdentifier(identifier))
	ac := downloadContext("mb-master", []string{"ac-master"})

	if err := step.Execute(context.Background(), ac); err != nil {
		t.Fatalf("acquisition must never block on a broken validator: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(ac.TempPath))
}

func TestDownloadStep_NoExpectedMBIDSkipsIdentificationEntirely(t *testing.T) {
	identifier := &stubIdentifier{match: ports.RecordingMatch{AcoustID: "ac-whatever"}}
	step := NewDownloadStep(&fileWritingSearcher{writeFile: true}, WithDownloadIdentifier(identifier))
	ac := downloadContext("", []string{"ac-master"})

	if err := step.Execute(context.Background(), ac); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(ac.TempPath))

	if identifier.calls != 0 {
		t.Errorf("identify calls = %d, want 0 with nothing to compare against", identifier.calls)
	}
}

func TestDownloadStep_RejectionWalksToTheNextCandidate(t *testing.T) {
	identifier := &rejectFirstIdentifier{cluster: []string{"ac-master"}}
	step := NewDownloadStep(&fileWritingSearcher{writeFile: true}, WithDownloadIdentifier(identifier))
	ac := downloadContext("mb-master", []string{"ac-master"})

	if err := step.Execute(context.Background(), ac); err != nil {
		t.Fatalf("a rejected candidate must not end the walk: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(ac.TempPath))

	if ac.Selected == nil || ac.Selected.URL != "https://youtube.com/watch?v=second00000" {
		t.Fatalf("selected = %+v, want the second candidate", ac.Selected)
	}
}

type rejectFirstIdentifier struct {
	cluster []string
	calls   int
}

func (r *rejectFirstIdentifier) Identify(context.Context, string) (ports.RecordingMatch, error) {
	r.calls++
	if r.calls == 1 {
		return ports.RecordingMatch{AcoustID: "ac-wrong", MBIDs: []string{"mb-wrong"}}, nil
	}
	return ports.RecordingMatch{AcoustID: "ac-master", MBIDs: []string{"mb-master"}}, nil
}

func (r *rejectFirstIdentifier) AcoustIDsFor(context.Context, string) ([]string, error) {
	return r.cluster, nil
}
