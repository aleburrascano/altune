package ytdlp

import (
	"fmt"
	"os"
	"syscall"
)

var errJarBusy error = syscall.EWOULDBLOCK

func tryLockJar(path string) (unlock func(), err error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open the cookie jar lock: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("lock the cookie jar: %w", err)
	}
	return func() { _ = f.Close() }, nil
}
