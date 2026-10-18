package main

import (
	"errors"
	"os"
	"syscall"
)

// Check the opened descriptor, not a path preflight: a FIFO (including one reached
// through a symlink or path replacement) must not block before its type is known.
// O_NONBLOCK has no effect on regular files. Regular-file symlinks remain supported.
func openRegularInput(path string) (*os.File, os.FileInfo, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, nil, err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, nil, err
	}
	if !info.Mode().IsRegular() {
		file.Close()
		return nil, nil, errors.New("input must be a regular file")
	}
	return file, info, nil
}
