package utils

import (
	"os"
	"path/filepath"
)

func ReadFile(dir, name string) (content string, path string) {
	path = filepath.Join(dir, name)
	b, err := os.ReadFile(path)
	if err != nil {
		return "", ""
	}
	return string(b), path
}
