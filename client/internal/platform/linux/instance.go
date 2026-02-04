//go:build linux

package linux

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
)

// InstanceLock provides single-instance enforcement on Linux
type InstanceLock struct {
	lockFile *os.File
	path     string
}

// NewInstanceLock creates a new instance lock
func NewInstanceLock(name string) *InstanceLock {
	// Try /var/run first, fall back to /tmp
	path := "/var/run/sfo-helper-" + name + ".lock"
	if _, err := os.Stat("/var/run"); os.IsNotExist(err) {
		path = filepath.Join(os.TempDir(), "sfo-helper-"+name+".lock")
	}
	return &InstanceLock{path: path}
}

// TryLock attempts to acquire the lock
// Returns true if this is the only instance, false if another instance exists
func (l *InstanceLock) TryLock() (bool, error) {
	file, err := os.OpenFile(l.path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return false, fmt.Errorf("failed to open lock file: %w", err)
	}

	// Try to acquire exclusive lock (non-blocking)
	err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		file.Close()
		if err == syscall.EWOULDBLOCK {
			return false, nil // Another instance holds the lock
		}
		return false, fmt.Errorf("failed to acquire lock: %w", err)
	}

	// Write our PID to the file
	file.Truncate(0)
	file.Seek(0, 0)
	file.WriteString(strconv.Itoa(os.Getpid()) + "\n")

	l.lockFile = file
	return true, nil
}

// Unlock releases the lock
func (l *InstanceLock) Unlock() error {
	if l.lockFile != nil {
		syscall.Flock(int(l.lockFile.Fd()), syscall.LOCK_UN)
		l.lockFile.Close()
		os.Remove(l.path)
		l.lockFile = nil
	}
	return nil
}
