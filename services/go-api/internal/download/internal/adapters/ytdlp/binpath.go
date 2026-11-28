package ytdlp

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

func ResolveBinary(name, dir string) string {
	if dir != "" {
		suffix := ""
		if runtime.GOOS == "windows" {
			suffix = ".exe"
		}
		full := filepath.Join(dir, name+suffix)
		if _, err := os.Stat(full); err == nil {
			return full
		}
	}
	return name
}

func BinaryRunnable(path string) bool {
	if filepath.IsAbs(path) {
		_, err := os.Stat(path)
		return err == nil
	}
	_, err := exec.LookPath(path)
	return err == nil
}
