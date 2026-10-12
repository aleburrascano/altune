package binpath

import (
	"os"
	"os/exec"
	"path/filepath"
)

var exeSuffix string

func Resolve(name, dir string) string {
	if dir != "" {
		full := filepath.Join(dir, name+exeSuffix)
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
