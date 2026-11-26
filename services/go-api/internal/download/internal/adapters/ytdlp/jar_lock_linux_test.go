package ytdlp_test

import (
	"altune/go-api/internal/download"
	"context"
	"encoding/binary"
	"errors"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
)

func holdJarLock(t *testing.T, jar string) (release func()) {
	t.Helper()
	f, err := os.OpenFile(jar+".lock", os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return func() { _ = f.Close() }
}

type lockOpens struct {
	fd    int
	count int
}

func watchLockOpens(t *testing.T, jar string) *lockOpens {
	t.Helper()
	fd, err := syscall.InotifyInit1(syscall.IN_NONBLOCK | syscall.IN_CLOEXEC)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Close(fd) })
	if _, err := syscall.InotifyAddWatch(fd, jar+".lock", syscall.IN_OPEN); err != nil {
		t.Fatal(err)
	}
	return &lockOpens{fd: fd}
}

func (w *lockOpens) seen(t *testing.T) int {
	t.Helper()
	buf := make([]byte, 4096)
	n, err := syscall.Read(w.fd, buf)
	if errors.Is(err, syscall.EAGAIN) {
		return w.count
	}
	if err != nil {
		t.Fatal(err)
	}
	for offset := 0; offset+syscall.SizeofInotifyEvent <= n; {
		w.count++
		offset += syscall.SizeofInotifyEvent + int(binary.NativeEndian.Uint32(buf[offset+12:offset+16]))
	}
	return w.count
}

func TestCookieJar_AWriteBackRetriesUntilAnotherProcessReleasesTheJarLock(t *testing.T) {
	jar := liveJar(t, signedInJar("old"))
	ytDlpRenewing(t, signedInJar("new"), "echo 212")
	release := holdJarLock(t, jar)
	opens := watchLockOpens(t, jar)
	done := make(chan error, 1)

	go func() { done <- canaryWith(jar) }()

	waitFor(t, "a second try at the jar lock another process holds", done, func() bool { return opens.seen(t) >= 2 })
	if got := jarContents(t, jar); got != signedInJar("old") {
		t.Fatalf("live jar = %q while another process held the lock, want it untouched", got)
	}
	release()
	if err := returnedWithin(t, waitLimit, "Canary() after the jar lock was released", done); err != nil {
		t.Fatalf("Canary() = %v, want nil", err)
	}
	if got := jarContents(t, jar); got != signedInJar("new") {
		t.Errorf("live jar = %q after the lock was released, want the renewed cookies", got)
	}
}

func TestCookieJar_AWriteBackGivesUpAndWarnsWhenTheRunEndsWhileTheJarIsLocked(t *testing.T) {
	logs := captureLogs(t)
	outDir := t.TempDir()
	jar := liveJar(t, signedInJar("old"))
	ytDlpRenewing(t, signedInJar("new"), stubDownload(outDir))
	holdJarLock(t, jar)
	opens := watchLockOpens(t, jar)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)

	go func() {
		_, err := download.NewYtDlpAudioSearcher("", jar, "").Download(ctx, youTubeWatchURL, outDir)
		done <- err
	}()
	waitFor(t, "a try at the jar lock another process holds", done, func() bool { return opens.seen(t) >= 1 })
	cancel()

	if err := returnedWithin(t, time.Second, "Download() after its context was canceled", done); err != nil {
		t.Errorf("Download() = %v, want the finished download returned", err)
	}
	if got := jarContents(t, jar); got != signedInJar("old") {
		t.Errorf("live jar = %q, want it untouched while another process held the lock", got)
	}
	if warned := loggedErrors(t, logs, "acquisition.cookie_jar_update_failed"); len(warned) != 1 || !strings.Contains(warned[0], "gave up waiting for the cookie jar lock: context canceled") {
		t.Errorf("update failure warnings = %q, want one saying it gave up on the lock when the run was canceled", warned)
	}
}

func TestCookieJar_AWriteBackForACallerWithNoDeadlineGivesUpOnALockThatIsNeverReleased(t *testing.T) {
	logs := captureLogs(t)
	outDir := t.TempDir()
	jar := liveJar(t, signedInJar("old"))
	ytDlpRenewing(t, signedInJar("new"), stubDownload(outDir))
	holdJarLock(t, jar)
	done := make(chan error, 1)

	go func() {
		_, err := download.NewYtDlpAudioSearcher("", jar, "").Download(context.Background(), youTubeWatchURL, outDir)
		done <- err
	}()

	if err := returnedWithin(t, 2*waitLimit, "Download() with no deadline", done); err != nil {
		t.Errorf("Download() = %v, want the finished download returned", err)
	}
	if got := jarContents(t, jar); got != signedInJar("old") {
		t.Errorf("live jar = %q, want it untouched while another process held the lock", got)
	}
	if warned := loggedErrors(t, logs, "acquisition.cookie_jar_update_failed"); len(warned) != 1 || !strings.Contains(warned[0], "gave up waiting for the cookie jar lock: context deadline exceeded") {
		t.Errorf("update failure warnings = %q, want one saying it gave up on the lock at its own deadline", warned)
	}
}
