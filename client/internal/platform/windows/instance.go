//go:build windows

package windows

import (
	"fmt"
	"syscall"
	"unsafe"
)

var (
	kernel32        = syscall.NewLazyDLL("kernel32.dll")
	createMutexW    = kernel32.NewProc("CreateMutexW")
	releaseMutex    = kernel32.NewProc("ReleaseMutex")
	closeHandle     = kernel32.NewProc("CloseHandle")
)

const (
	ERROR_ALREADY_EXISTS = 183
)

// InstanceLock provides single-instance enforcement on Windows
type InstanceLock struct {
	handle syscall.Handle
	name   string
}

// NewInstanceLock creates a new instance lock
func NewInstanceLock(name string) *InstanceLock {
	return &InstanceLock{name: name}
}

// TryLock attempts to acquire the lock
// Returns true if this is the only instance, false if another instance exists
func (l *InstanceLock) TryLock() (bool, error) {
	mutexName, err := syscall.UTF16PtrFromString("Global\\SFOHelper_" + l.name)
	if err != nil {
		return false, fmt.Errorf("failed to create mutex name: %w", err)
	}

	handle, _, err := createMutexW.Call(
		0,
		0,
		uintptr(unsafe.Pointer(mutexName)),
	)

	if handle == 0 {
		return false, fmt.Errorf("failed to create mutex: %w", err)
	}

	l.handle = syscall.Handle(handle)

	// Check if mutex already existed
	if err == syscall.Errno(ERROR_ALREADY_EXISTS) {
		closeHandle.Call(uintptr(l.handle))
		l.handle = 0
		return false, nil
	}

	return true, nil
}

// Unlock releases the lock
func (l *InstanceLock) Unlock() error {
	if l.handle != 0 {
		releaseMutex.Call(uintptr(l.handle))
		closeHandle.Call(uintptr(l.handle))
		l.handle = 0
	}
	return nil
}
