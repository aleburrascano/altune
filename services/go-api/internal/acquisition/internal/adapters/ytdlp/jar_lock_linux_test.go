package ytdlp_test

import (
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func holdJarLock(t *testing.T, jar string) (lockInode uint64, release func()) {
	t.Helper()
	f, err := os.OpenFile(jar+".lock", os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	var stat syscall.Stat_t
	if err := syscall.Fstat(int(f.Fd()), &stat); err != nil {
		t.Fatal(err)
	}
	return stat.Ino, func() { _ = f.Close() }
}

func someoneWaitsForLock(t *testing.T, inode uint64) bool {
	t.Helper()
	locks, err := os.ReadFile("/proc/locks")
	if err != nil {
		t.Fatal(err)
	}
	suffix := ":" + strconv.FormatUint(inode, 10)
	for line := range strings.Lines(string(locks)) {
		fields := strings.Fields(line)
		if len(fields) > 6 && fields[1] == "->" && fields[2] == "FLOCK" && strings.HasSuffix(fields[6], suffix) {
			return true
		}
	}
	return false
}

func TestCookieJar_AWriteBackWaitsWhileAnotherProcessHoldsTheJarLock(t *testing.T) {
	jar := liveJar(t, signedInJar("old"))
	ytDlpRenewing(t, signedInJar("new"), "echo 212")
	inode, release := holdJarLock(t, jar)
	done := make(chan error, 1)

	go func() { done <- canaryWith(jar) }()

	for !someoneWaitsForLock(t, inode) {
		select {
		case err := <-done:
			release()
			t.Fatalf("Canary() returned %v while another process held the jar lock, want it to wait for the lock", err)
		case <-time.After(time.Millisecond):
		}
	}
	if got := jarContents(t, jar); got != signedInJar("old") {
		t.Fatalf("live jar = %q while another process held the lock, want it untouched", got)
	}
	release()
	if err := <-done; err != nil {
		t.Fatalf("Canary() = %v, want nil", err)
	}
	if got := jarContents(t, jar); got != signedInJar("new") {
		t.Errorf("live jar = %q after the lock was released, want the renewed cookies", got)
	}
}
