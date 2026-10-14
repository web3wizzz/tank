package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
)

// Publish private bytes only after syncing, without overwriting destinations.
func savePrivateNew(data []byte, destination string) error {
	file, err := os.CreateTemp(filepath.Dir(destination), ".tank-private-")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if _, err = file.Write(data); err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	if err = os.Link(file.Name(), destination); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(destination))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func readRecoveryFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 2048 {
		return nil, errors.New("invalid recovery file")
	}
	data, err := io.ReadAll(io.LimitReader(file, 2049))
	if len(data) > 2048 {
		clear(data)
		return nil, errors.New("recovery file is too large")
	}
	return data, err
}
