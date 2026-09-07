package binpath

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

func Resolve(name, dir string) string {
	if dir != "" {
		candidate := name
		if runtime.GOOS == "windows" {
			candidate = name + ".exe"
		}
		full := filepath.Join(dir, candidate)
		if _, err := os.Stat(full); err == nil {
			return full
		}
	}
	return name
}

func Runnable(path string) bool {
	if filepath.IsAbs(path) {
		_, err := os.Stat(path)
		return err == nil
	}
	_, err := exec.LookPath(path)
	return err == nil
}
