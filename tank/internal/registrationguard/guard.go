// Package registrationguard fences the entire signing process against restore
// controllers. SQLite leases alone fence persistence, not calls to custody.
package registrationguard

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

var ErrBlocked = errors.New("registration lane busy or maintenance blocked")

type Guard struct{ file *os.File }

// Acquire uses a stable, operator-owned lane path. Never remove the lock inode:
// unlinking it would let a second process acquire a different lock.
func Acquire(path string) (*Guard, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, ErrBlocked
	}
	for current := filepath.Clean(path); ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return nil, ErrBlocked
		}
		if err != nil && !(current == path && os.IsNotExist(err)) {
			return nil, ErrBlocked
		}
		if current == filepath.Dir(current) {
			break
		}
	}
	parent, err := os.Stat(filepath.Dir(path))
	if err != nil || !parent.IsDir() || parent.Mode().Perm()&0077 != 0 {
		return nil, ErrBlocked
	}
	owner, ok := parent.Sys().(*syscall.Stat_t)
	if !ok || owner.Uid != uint32(os.Geteuid()) {
		return nil, ErrBlocked
	}
	fd, err := syscall.Open(path, syscall.O_CREAT|syscall.O_RDWR|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0600)
	if err != nil {
		return nil, ErrBlocked
	}
	file := os.NewFile(uintptr(fd), path)
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		file.Close()
		return nil, ErrBlocked
	}
	owner, ok = info.Sys().(*syscall.Stat_t)
	if !ok || owner.Uid != uint32(os.Geteuid()) || owner.Nlink != 1 {
		file.Close()
		return nil, ErrBlocked
	}
	if syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		file.Close()
		return nil, ErrBlocked
	}
	if _, err := os.Lstat(path + ".restore-gate"); !os.IsNotExist(err) {
		file.Close()
		return nil, ErrBlocked
	}
	return &Guard{file: file}, nil
}

func (g *Guard) Close() error { return g.file.Close() }
