package ytdlp

import "errors"

var errJarBusy = errors.New("cookie jar is locked")

func tryLockJar(string) (unlock func(), err error) {
	return func() {}, nil
}
