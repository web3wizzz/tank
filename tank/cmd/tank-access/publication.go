package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func syncCredentialDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	err = directory.Sync()
	closeErr := directory.Close()
	if err != nil {
		return err
	}
	return closeErr
}

// Preserve the existing automatic parent creation, but flush every newly created
// directory and its entry in the first existing ancestor before issuing a key.
// Parents must remain under exclusive operator control for the whole operation.
func prepareCredentialDirectory(path string) error {
	var missing []string
	ancestor := filepath.Clean(path)
	for {
		info, err := os.Stat(ancestor)
		if err == nil {
			if !info.IsDir() {
				return fmt.Errorf("credential parent must be a directory")
			}
			break
		}
		if !os.IsNotExist(err) {
			return err
		}
		missing = append(missing, ancestor)
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return err
		}
		ancestor = parent
	}
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	if len(missing) == 0 {
		return nil
	}
	for _, directory := range append(missing, ancestor) {
		if err := syncCredentialDirectory(directory); err != nil {
			return err
		}
	}
	return nil
}
