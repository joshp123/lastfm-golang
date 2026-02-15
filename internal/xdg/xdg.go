package xdg

import (
	"errors"
	"os"
	"path/filepath"
)

func DataHome() (string, error) {
	if v := os.Getenv("XDG_DATA_HOME"); v != "" {
		return v, nil
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if h == "" {
		return "", errors.New("empty home dir")
	}
	return filepath.Join(h, ".local", "share"), nil
}
